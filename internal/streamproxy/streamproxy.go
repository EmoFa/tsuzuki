// Package streamproxy serves remote HLS/MP4 streams to the player from localhost.
//
// Some stream CDNs reject HTTP/1.1, which is all mpv/ffmpeg speak. The proxy
// accepts the player's HTTP/1.1 requests and refetches upstream with Go's
// HTTP/2 client, adding the stream's required headers. Playlists are decoded
// when a provider obfuscates them, optionally narrowed to one video variant, and
// rewritten so keys, segments and variant playlists also go through the proxy.
package streamproxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/hls"
	"github.com/EmoFa/anitui/internal/httpx"
)

const (
	// maxPlaylist bounds how much of a playlist is buffered for rewriting.
	maxPlaylist = 16 << 20
	// maxWrapper is how far into a wrapped segment the MPEG-TS data may start.
	maxWrapper = 64 << 10
	tsPacket   = 188
)

// forwardedResponseHeaders are copied from upstream for pass-through bodies.
var forwardedResponseHeaders = []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"}

type Proxy struct {
	client *httpx.Client
	ln     net.Listener
	srv    *http.Server
	base   string

	mu       sync.RWMutex
	sessions map[string]*session
}

// session is everything needed to fetch one stream and its sub-resources.
type session struct {
	headers       map[string]string
	codec         *domain.PlaylistCodec
	variantHeight int
	unwrapTS      bool
}

// Start listens on a random localhost port. client must not have an overall
// request timeout (httpx.Options.Timeout < 0), or long bodies are cut off.
func Start(client *httpx.Client) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		client:   client,
		ln:       ln,
		base:     "http://" + ln.Addr().String(),
		sessions: map[string]*session{},
	}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := p.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("stream proxy stopped", "err", err)
		}
	}()
	return p, nil
}

// URL registers upstream with the headers it needs and returns the local URL
// the player should open.
func (p *Proxy) URL(upstream string, headers map[string]string) string {
	return p.register(upstream, &session{headers: headers})
}

// Stream registers a provider stream, honouring its playlist codec and
// variant selection, and returns the local URL the player should open.
func (p *Proxy) Stream(s domain.Stream) string {
	return p.register(s.URL, &session{headers: s.Headers, codec: s.Playlist, variantHeight: s.VariantHeight, unwrapTS: s.WrappedSegments})
}

func (p *Proxy) register(upstream string, s *session) string {
	id := newSessionID()
	p.mu.Lock()
	p.sessions[id] = s
	p.mu.Unlock()
	return p.localURL(id, upstream)
}

func (p *Proxy) Close() error { return p.srv.Close() }

// localURL encodes upstream into the path. The upstream file name is appended
// so players can still sniff the format from the extension.
func (p *Proxy) localURL(session, upstream string) string {
	name := "stream"
	if u, err := url.Parse(upstream); err == nil && path.Base(u.Path) != "/" && path.Base(u.Path) != "." {
		name = path.Base(u.Path)
	}
	return p.base + "/s/" + session + "/" + base64.RawURLEncoding.EncodeToString([]byte(upstream)) + "/" + url.PathEscape(name)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/s/"), "/", 3)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	p.mu.RLock()
	sess, ok := p.sessions[id]
	p.mu.RUnlock()
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	upstream := string(raw)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for k, v := range sess.headers {
		req.Header.Set(k, v)
	}
	// Players request playlists with "Range: bytes=0-". Forwarding that gets a 206
	// from some CDNs; playlists are always fetched whole so they can be rewritten.
	// Wrapped segments are always fetched whole: a range would miss the offset.
	if rng := r.Header.Get("Range"); rng != "" && !hasPlaylistExt(upstream) && !sess.unwrapTS {
		req.Header.Set("Range", rng)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			slog.Warn("proxy upstream failed", "url", upstream, "err", err)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body := bufio.NewReaderSize(resp.Body, maxWrapper+3*tsPacket)
	peek, _ := body.Peek(64)
	ok2xx := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent
	encoded := sess.codec != nil && bytes.HasPrefix(peek, sess.codec.Prefix)
	if ok2xx && (encoded || isPlaylist(upstream, resp.Header.Get("Content-Type"), peek)) {
		p.servePlaylist(w, r, id, sess, upstream, body, encoded)
		return
	}

	if resp.StatusCode >= 400 {
		slog.Warn("proxy upstream status", "url", upstream, "status", resp.StatusCode)
	}
	if ok2xx && sess.unwrapTS {
		if head, _ := body.Peek(maxWrapper + 3*tsPacket); !isTS(head) {
			if off := tsStart(head); off > 0 {
				body.Discard(off)
				w.Header().Set("Content-Type", "video/mp2t")
				if resp.ContentLength > int64(off) && resp.StatusCode == http.StatusOK {
					w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength-int64(off), 10))
				}
				w.WriteHeader(http.StatusOK)
				if r.Method == http.MethodGet {
					io.Copy(w, body)
				}
				return
			}
		}
	}
	for _, h := range forwardedResponseHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodGet {
		io.Copy(w, body)
	}
}

func (p *Proxy) servePlaylist(w http.ResponseWriter, r *http.Request, id string, sess *session, upstream string, body io.Reader, encoded bool) {
	data, err := io.ReadAll(io.LimitReader(body, maxPlaylist))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if encoded {
		if data, err = sess.codec.Decode(data); err != nil {
			slog.Warn("proxy playlist decode failed", "url", upstream, "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if sess.variantHeight > 0 {
		data = hls.KeepVariant(data, sess.variantHeight)
	}
	base, _ := url.Parse(upstream)
	rewritten := hls.Rewrite(data, base, func(abs string) string { return p.localURL(id, abs) })
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	if r.Method == http.MethodGet {
		w.Write(rewritten)
	}
}

func isPlaylist(upstream, contentType string, peek []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") {
		return true
	}
	return hasPlaylistExt(upstream) || hls.IsPlaylist(peek)
}

func hasPlaylistExt(upstream string) bool {
	u, err := url.Parse(upstream)
	return err == nil && strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

// isTS reports whether b starts on MPEG-TS packet boundaries.
func isTS(b []byte) bool { return tsAligned(b, 0) }

// tsStart finds where aligned MPEG-TS packets begin in b, or -1.
func tsStart(b []byte) int {
	for i := 0; i+2*tsPacket < len(b) && i < maxWrapper; i++ {
		if tsAligned(b, i) {
			return i
		}
	}
	return -1
}

// tsAligned checks for three sync bytes a packet apart, starting at off.
func tsAligned(b []byte, off int) bool {
	return off+2*tsPacket < len(b) && b[off] == 0x47 && b[off+tsPacket] == 0x47 && b[off+2*tsPacket] == 0x47
}

func newSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
