package streamcheck

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
)

// server serves a small HLS tree. Segment responses can be switched to failures.
func server(t *testing.T, segment func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://ref/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/master.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=1920x1080\nhi/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=854x480\nlo/index.m3u8\n")
		case "/hi/index.m3u8", "/lo/index.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXTINF:10,\nseg0.ts\n#EXT-X-ENDLIST\n")
		case "/encrypted.txt":
			io.WriteString(w, "ENC:"+base64.StdEncoding.EncodeToString([]byte("#EXTM3U\n#EXTINF:10,\n/hi/seg0.ts\n")))
		case "/empty.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-ENDLIST\n")
		case "/hi/seg0.ts", "/lo/seg0.ts":
			segment(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func goodSegment(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Range") != "bytes=0-1023" {
		http.Error(w, "range expected", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusPartialContent)
	w.Write([]byte{0x47, 0x40, 0x11, 0x10})
}

func TestCheck(t *testing.T) {
	headers := map[string]string{"Referer": "https://ref/"}
	codec := &domain.PlaylistCodec{Prefix: []byte("ENC:"), Decode: func(b []byte) ([]byte, error) {
		return base64.StdEncoding.DecodeString(strings.TrimPrefix(string(b), "ENC:"))
	}}

	ok := server(t, goodSegment)
	c := &Checker{Client: httpx.New(httpx.Options{Retries: 1})}
	ctx := context.Background()

	for name, s := range map[string]domain.Stream{
		"master picks variant": {URL: ok.URL + "/master.m3u8", Kind: domain.HLS, Headers: headers, VariantHeight: 480},
		"encrypted playlist":   {URL: ok.URL + "/encrypted.txt", Kind: domain.HLS, Headers: headers, Playlist: codec},
		"mp4":                  {URL: ok.URL + "/hi/seg0.ts", Kind: domain.MP4, Headers: headers},
	} {
		if err := c.Check(ctx, s); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	for name, tc := range map[string]struct {
		stream  domain.Stream
		segment func(http.ResponseWriter, *http.Request)
		want    string
	}{
		"missing headers": {domain.Stream{URL: ok.URL + "/master.m3u8", Kind: domain.HLS}, goodSegment, "HTTP 403"},
		"no segments":     {domain.Stream{URL: ok.URL + "/empty.m3u8", Kind: domain.HLS, Headers: headers}, goodSegment, "no segments"},
		"not a playlist": {domain.Stream{URL: ok.URL + "/hi/seg0.ts", Kind: domain.HLS, Headers: headers},
			func(w http.ResponseWriter, r *http.Request) { w.Write([]byte{0x47, 0x40, 0x11, 0x10}) }, "not a playlist"},
		"dead segment":     {domain.Stream{URL: ok.URL + "/master.m3u8", Kind: domain.HLS, Headers: headers}, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", http.StatusGone) }, "first segment"},
		"html instead":     {domain.Stream{URL: ok.URL + "/master.m3u8", Kind: domain.HLS, Headers: headers}, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "<!DOCTYPE html><html>blocked") }, "web page"},
		"encrypted no key": {domain.Stream{URL: ok.URL + "/encrypted.txt", Kind: domain.HLS, Headers: headers}, goodSegment, "not a playlist"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := server(t, tc.segment)
			tc.stream.URL = strings.Replace(tc.stream.URL, ok.URL, srv.URL, 1)
			err := c.Check(ctx, tc.stream)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}
