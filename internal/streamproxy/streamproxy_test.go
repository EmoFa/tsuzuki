package streamproxy

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
)

func TestProxyRewritesPlaylistAndForwardsSegments(t *testing.T) {
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Referer() != "https://kwik.cx/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v/uwu.m3u8":
			if r.Header.Get("Range") != "" {
				// Like the real CDN: a ranged request gets a 206 the proxy must
				// not pass through unrewritten.
				t.Errorf("Range forwarded for playlist: %q", r.Header.Get("Range"))
			}
			// Served as text/plain to check detection by extension/content.
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\""+upstream.URL+"/v/mon.key\"\n#EXTINF:10,\nseg-1.jpg\n#EXT-X-ENDLIST\n")
		case "/v/mon.key":
			io.WriteString(w, "0123456789abcdef")
		case "/v/seg-1.jpg":
			if r.Header.Get("Range") != "bytes=2-" {
				t.Errorf("range not forwarded: %q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 2-4/5")
			w.WriteHeader(http.StatusPartialContent)
			io.WriteString(w, "SEG")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	p, err := Start(httpx.New(httpx.Options{Timeout: -1}))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	local := p.URL(upstream.URL+"/v/uwu.m3u8", map[string]string{"Referer": "https://kwik.cx/"})
	if !strings.HasSuffix(local, "/uwu.m3u8") {
		t.Errorf("local URL should keep file name: %s", local)
	}

	// ffmpeg requests playlists with an open-ended range.
	playlist := get(t, local, "bytes=0-")
	if strings.Contains(playlist, upstream.URL) {
		t.Fatalf("upstream URL leaked into playlist:\n%s", playlist)
	}
	var keyURL, segURL string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "#EXT-X-KEY") {
			keyURL = line[strings.Index(line, `URI="`)+5 : strings.LastIndex(line, `"`)]
		} else if strings.HasPrefix(line, "http") {
			segURL = line
		}
	}
	if got := get(t, keyURL, ""); got != "0123456789abcdef" {
		t.Errorf("key = %q", got)
	}
	if got := get(t, segURL, "bytes=2-"); got != "SEG" {
		t.Errorf("segment = %q", got)
	}
}

func TestProxyRewritesPartialContentPlaylist(t *testing.T) {
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An extensionless playlist can't be recognised up front, so its Range is
		// forwarded and the CDN answers 206.
		w.Header().Set("Content-Range", "bytes 0-39/40")
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, "#EXTM3U\n#EXTINF:10,\n"+upstream.URL+"/seg\n")
	}))
	defer upstream.Close()

	p, err := Start(httpx.New(httpx.Options{Timeout: -1}))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	playlist := get(t, p.URL(upstream.URL+"/playlist", nil), "bytes=0-")
	if strings.Contains(playlist, upstream.URL) {
		t.Fatalf("206 playlist not rewritten:\n%s", playlist)
	}
}

func TestProxyDecodesPlaylistsAndSelectsVariant(t *testing.T) {
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",LANGUAGE=\"en\",URI=\"audio.txt\"\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=1920x1080,AUDIO=\"a\"\nhi.txt\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=854x480,AUDIO=\"a\"\nlo.txt\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ENC:"+base64.StdEncoding.EncodeToString([]byte(master)))
	}))
	defer upstream.Close()

	p, err := Start(httpx.New(httpx.Options{Timeout: -1}))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	codec := &domain.PlaylistCodec{
		Prefix: []byte("ENC:"),
		Decode: func(b []byte) ([]byte, error) {
			return base64.StdEncoding.DecodeString(strings.TrimPrefix(string(b), "ENC:"))
		},
	}
	local := p.Stream(domain.Stream{URL: upstream.URL + "/master.txt", Playlist: codec, VariantHeight: 480})
	got := get(t, local, "bytes=0-")
	if !strings.HasPrefix(got, "#EXTM3U") || strings.Contains(got, "hi.txt") || strings.Contains(got, "1080") {
		t.Fatalf("playlist not decoded/filtered:\n%s", got)
	}
	if strings.Count(got, p.base) != 2 { // audio rendition + 480p variant
		t.Fatalf("expected 2 proxied URIs:\n%s", got)
	}
}

func TestProxyRejectsUnknownSession(t *testing.T) {
	p, err := Start(httpx.New(httpx.Options{Timeout: -1}))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	resp, err := http.Get(p.base + "/s/nope/aHR0cHM6Ly9leGFtcGxlLmNvbQ/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func get(t *testing.T, url, rng string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	// Force HTTP/1.1 like mpv does.
	client := &http.Client{Transport: &http.Transport{Protocols: http1Only()}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, body)
	}
	return string(body)
}

func http1Only() *http.Protocols {
	var p http.Protocols
	p.SetHTTP1(true)
	return &p
}
