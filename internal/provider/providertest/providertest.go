// Package providertest is a contract test suite every provider runs, against
// recorded fixtures in unit tests and against the real site in live tests.
package providertest

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/provider"
)

type Case struct {
	Query string
	Mode  domain.Mode
	// ShowTitle picks the search result to continue with: an exact title match,
	// else the first result containing it.
	ShowTitle string
	// MinEpisodes is the least number of episodes the show must list.
	MinEpisodes int
	// Episode is the canonical number to resolve streams for.
	Episode float64
	// StreamsUnavailable expects Streams to fail with ErrStreamsUnavailable.
	StreamsUnavailable bool
}

// Run checks the invariants other packages rely on.
func Run(t *testing.T, p provider.Provider, c Case) {
	t.Helper()
	ctx := context.Background()

	shows, err := p.Search(ctx, c.Query, c.Mode)
	if err != nil {
		t.Fatalf("Search(%q): %v", c.Query, err)
	}
	if len(shows) == 0 {
		t.Fatalf("Search(%q) returned nothing", c.Query)
	}
	var show *domain.Show
	for i, s := range shows {
		if s.ID == "" || s.Title == "" {
			t.Errorf("result %d missing ID or title: %+v", i, s)
		}
		if s.Provider != p.Name() {
			t.Errorf("result %d provider = %q, want %q", i, s.Provider, p.Name())
		}
		if s.Title == c.ShowTitle || (show == nil && strings.Contains(s.Title, c.ShowTitle)) {
			if show == nil || show.Title != c.ShowTitle {
				show = &shows[i]
			}
		}
	}
	if show == nil {
		t.Fatalf("no result titled %q", c.ShowTitle)
	}

	eps, err := p.Episodes(ctx, show.ID, c.Mode)
	if err != nil {
		t.Fatalf("Episodes(%s): %v", show.ID, err)
	}
	if len(eps) < c.MinEpisodes {
		t.Fatalf("Episodes returned %d, want at least %d", len(eps), c.MinEpisodes)
	}
	seen := map[float64]bool{}
	for i, e := range eps {
		if e.ID == "" {
			t.Errorf("episode %d has no ID", i)
		}
		if seen[e.Number] {
			t.Errorf("duplicate episode number %v", e.Number)
		}
		seen[e.Number] = true
		if i > 0 && eps[i-1].Number >= e.Number {
			t.Errorf("episodes not sorted: %v before %v", eps[i-1].Number, e.Number)
		}
	}

	ep, err := provider.FindEpisode(eps, c.Episode)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := p.Streams(ctx, show.ID, ep, c.Mode)
	if c.StreamsUnavailable {
		if !errors.Is(err, provider.ErrStreamsUnavailable) {
			t.Fatalf("Streams err = %v, want ErrStreamsUnavailable", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Streams(ep %v): %v", c.Episode, err)
	}
	if len(streams) == 0 {
		t.Fatal("Streams returned nothing")
	}
	for i, s := range streams {
		if !strings.HasPrefix(s.URL, "http") {
			t.Errorf("stream %d URL = %q", i, s.URL)
		}
		if s.Kind != domain.HLS && s.Kind != domain.MP4 {
			t.Errorf("stream %d kind = %q", i, s.Kind)
		}
		if s.Audio != c.Mode {
			t.Errorf("stream %d audio = %q, want %q", i, s.Audio, c.Mode)
		}
	}
}

// Routes is an http.RoundTripper serving canned responses keyed by
// "host/path?query" (query omitted when empty). Unknown URLs fail the test.
type Routes struct {
	T      *testing.T
	Routes map[string]func(*http.Request) (*http.Response, error)
}

func (rt Routes) RoundTrip(r *http.Request) (*http.Response, error) {
	key := r.URL.Host + r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	h, ok := rt.Routes[key]
	if !ok {
		rt.T.Errorf("unexpected request: %s %s", r.Method, key)
		return Respond(r, http.StatusNotFound, "text/plain", nil), nil
	}
	return h(r)
}

// Respond builds a response for a Routes handler.
func Respond(r *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {contentType}},
		Body:       nopCloser{strings.NewReader(string(body))},
		Request:    r,
	}
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
