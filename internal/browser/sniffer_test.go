package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
)

func TestSnifferCapturesRequestsAndBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if _, ok := launcher.LookPath(); !ok {
		t.Skip("no Chrome/Chromium installed")
	}
	var referer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embed":
			referer = r.Referer()
			w.Write([]byte(`<html><body><script>
				fetch("/api/getSources?id=1").then(r => r.json()).then(j => fetch(j.next));
			</script></body></html>`))
		case "/api/getSources":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"next":"/media/master.m3u8?token=abc"}`))
		case "/media/master.m3u8":
			w.Write([]byte("#EXTM3U\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := &Sniffer{}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	got, err := s.Sniff(ctx, SniffRequest{
		URL: srv.URL + "/embed",
		// http: Chromium drops an https referrer when navigating to an http page.
		Referer: "http://example.org/",
		Matches: []Match{
			{Name: "sources", URL: regexp.MustCompile(`/getSources`), Body: true},
			{Name: "master", URL: regexp.MustCompile(`master\.m3u8`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got["sources"].Body) != `{"next":"/media/master.m3u8?token=abc"}` || got["sources"].Status != 200 {
		t.Errorf("sources = %+v", got["sources"])
	}
	if got["master"].URL != srv.URL+"/media/master.m3u8?token=abc" {
		t.Errorf("master = %+v", got["master"])
	}
	if referer != "http://example.org/" {
		t.Errorf("referer = %q", referer)
	}

	// The browser is reused; a page that never makes the request times out.
	_, err = s.Sniff(ctx, SniffRequest{
		URL:     srv.URL + "/nothing",
		Matches: []Match{{Name: "master", URL: regexp.MustCompile(`master\.m3u8`)}},
		Timeout: 2 * time.Second,
	})
	if err == nil {
		t.Error("expected timeout error")
	}
}
