// Package streamcheck verifies a stream actually serves media before it is
// handed to the player, so a dead source is skipped in seconds instead of mpv
// failing after a long wait.
package streamcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/hls"
	"github.com/EmoFa/tsuzuki/internal/httpx"
)

// maxPlaylist bounds how much of a playlist is read.
const maxPlaylist = 4 << 20

// probeBytes is how much of a segment or file is requested.
const probeBytes = 1024

type Checker struct {
	Client *httpx.Client
}

// Check fetches the stream's playlist chain and the start of its first segment
// (or of the file itself), failing on errors, HTML pages and empty playlists.
func (c *Checker) Check(ctx context.Context, s domain.Stream) error {
	if s.Kind == domain.MP4 {
		return c.probe(ctx, s.URL, s.Headers)
	}

	playlistURL := s.URL
	body, err := c.playlist(ctx, playlistURL, s)
	if err != nil {
		return err
	}
	base, err := url.Parse(playlistURL)
	if err != nil {
		return err
	}

	if variants := hls.Variants(body, base); len(variants) > 0 {
		v := variants[0]
		for _, cand := range variants {
			if s.VariantHeight > 0 && cand.Height == s.VariantHeight {
				v = cand
				break
			}
		}
		if body, err = c.playlist(ctx, v.URI, s); err != nil {
			return fmt.Errorf("variant playlist: %w", err)
		}
		if base, err = url.Parse(v.URI); err != nil {
			return err
		}
	}

	segment := firstSegment(body, base)
	if segment == "" {
		return errors.New("playlist lists no segments")
	}
	if err := c.probe(ctx, segment, s.Headers); err != nil {
		return fmt.Errorf("first segment: %w", err)
	}
	return nil
}

func (c *Checker) playlist(ctx context.Context, u string, s domain.Stream) ([]byte, error) {
	body, err := c.fetch(ctx, u, s.Headers, "")
	if err != nil {
		return nil, err
	}
	if s.Playlist != nil && bytes.HasPrefix(body, s.Playlist.Prefix) {
		if body, err = s.Playlist.Decode(body); err != nil {
			return nil, fmt.Errorf("decoding playlist: %w", err)
		}
	}
	if !hls.IsPlaylist(body) {
		return nil, fmt.Errorf("%s is not a playlist (starts %q)", u, snippet(body))
	}
	return body, nil
}

// probe requests the first bytes of a media URL.
func (c *Checker) probe(ctx context.Context, u string, headers map[string]string) error {
	body, err := c.fetch(ctx, u, headers, fmt.Sprintf("bytes=0-%d", probeBytes-1))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("%s returned no data", u)
	}
	if looksLikeHTML(body) {
		return fmt.Errorf("%s returned a web page instead of media", u)
	}
	return nil
}

func (c *Checker) fetch(ctx context.Context, u string, headers map[string]string, rng string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	limit := int64(maxPlaylist)
	if rng != "" {
		limit = probeBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return body, nil
}

// firstSegment returns the first media URI of a media playlist, resolved.
func firstSegment(body []byte, base *url.URL) string {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if u, err := base.Parse(line); err == nil {
			return u.String()
		}
		return line
	}
	return ""
}

func looksLikeHTML(b []byte) bool {
	trimmed := bytes.ToLower(bytes.TrimSpace(b))
	return bytes.HasPrefix(trimmed, []byte("<!doctype html")) || bytes.HasPrefix(trimmed, []byte("<html"))
}

func snippet(b []byte) string {
	if len(b) > 24 {
		b = b[:24]
	}
	return string(b)
}
