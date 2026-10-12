package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/sniff"
)

type browserHost struct {
	fakeHost
	sniffed string
	sniff   func(string) (string, error)
	solve   func(string) (string, error)
}

func (h *browserHost) Sniff(req string) (string, error) {
	h.sniffed = req
	return h.sniff(req)
}

func (h *browserHost) Solve(url string) (string, error) { return h.solve(url) }

func TestHostSnifferPassesMatchesAndReadsCaptures(t *testing.T) {
	h := &browserHost{sniff: func(string) (string, error) {
		// Body is base64, as Go's encoding/json writes []byte.
		return `{"sources":{"URL":"https://x/stream/getSources?id=1","Status":200,"Body":"eyJ0cmFja3MiOltdfQ=="},
			"master":{"URL":"https://cdn/master.m3u8","Status":200}}`, nil
	}}
	got, err := hostSniffer{h}.Sniff(t.Context(), sniff.Request{
		URL:     "https://megaplay.buzz/stream/s-2/1/sub",
		Referer: "https://anikoto.tv/",
		Matches: []sniff.Match{
			{Name: "sources", URL: regexp.MustCompile(`/stream/getSources\?`), Body: true},
			{Name: "master", URL: regexp.MustCompile(`\.m3u8(\?|$)`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got["sources"].Body) != `{"tracks":[]}` || got["master"].URL != "https://cdn/master.m3u8" {
		t.Errorf("captured %+v", got)
	}
	var req sniffRequest
	if err := json.Unmarshal([]byte(h.sniffed), &req); err != nil {
		t.Fatal(err)
	}
	if req.TimeoutMillis != 30000 || len(req.Matches) != 2 || req.Matches[0].Pattern != `/stream/getSources\?` ||
		!req.Matches[0].Body || len(req.Blocked) == 0 {
		t.Errorf("request %s", h.sniffed)
	}
}

// A cancelled watch doesn't wait for the WebView to give up.
func TestHostSnifferStopsWithItsContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := &browserHost{sniff: func(string) (string, error) {
		<-release
		return "{}", nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := hostSniffer{h}.Sniff(ctx, sniff.Request{URL: "https://x", Timeout: time.Minute})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestHostSolver(t *testing.T) {
	h := &browserHost{solve: func(url string) (string, error) {
		return `{"UserAgent":"Mozilla/5.0 (Linux; Android 16) Chrome/152","Cookies":[{"Name":"cf_clearance","Value":"abc"}]}`, nil
	}}
	cl, err := hostSolver{h}.Solve(t.Context(), "https://animepahe.pw/")
	if err != nil {
		t.Fatal(err)
	}
	if cl.UserAgent == "" || len(cl.Cookies) != 1 || cl.Cookies[0].Name != "cf_clearance" || cl.ObtainedAt.IsZero() {
		t.Errorf("clearance %+v", cl)
	}

	// A check the user abandoned leaves no cookies, which is a failure.
	h.solve = func(string) (string, error) { return `{"UserAgent":"x","Cookies":[]}`, nil }
	if _, err := (hostSolver{h}).Solve(t.Context(), "https://animepahe.pw/"); err == nil {
		t.Error("an unfinished check counted as solved")
	}
}
