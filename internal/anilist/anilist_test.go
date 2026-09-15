package anilist

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EmoFa/anitui/internal/httpx"
)

type memKV struct {
	mu sync.Mutex
	m  map[string]string
	t  map[string]time.Time
}

func newMemKV() *memKV { return &memKV{m: map[string]string{}, t: map[string]time.Time{}} }

func (k *memKV) GetKV(_ context.Context, key string) (string, time.Time, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return v, k.t[key], ok, nil
}

func (k *memKV) PutKV(_ context.Context, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key], k.t[key] = value, time.Now()
	return nil
}

// server runs handler with each request body against a test AniList endpoint.
func server(t *testing.T, handler func(w http.ResponseWriter, body string)) (*Client, *memKV) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		handler(w, string(body))
	}))
	t.Cleanup(srv.Close)
	kv := newMemKV()
	c := New(httpx.New(httpx.Options{Backoff: time.Millisecond}), kv)
	c.url = srv.URL
	return c, kv
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSearchAndCachesResults(t *testing.T) {
	search := fixture(t, "search_frieren.json")
	c, kv := server(t, func(w http.ResponseWriter, body string) {
		if !strings.Contains(body, `"search":"frieren"`) || !strings.Contains(body, "isAdult: false") {
			t.Errorf("unexpected body %s", body)
		}
		w.Write(search)
	})
	res, err := c.Search(context.Background(), "frieren", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) < 3 || res[0].ID != 154587 || res[0].IDMal != 52991 || res[0].Episodes != 28 {
		t.Fatalf("first = %+v", res[0])
	}
	if res[0].DisplayTitle() != "Frieren: Beyond Journey’s End" {
		t.Errorf("display title = %q", res[0].DisplayTitle())
	}
	if _, _, ok, _ := kv.GetKV(context.Background(), "anilist:media:154587"); !ok {
		t.Error("search results not cached")
	}
	// A result without a MAL ID decodes to 0.
	for _, m := range res {
		if m.ID == 189513 && m.IDMal != 0 {
			t.Errorf("idMal = %d", m.IDMal)
		}
	}
}

func TestMediaUsesFreshCacheThenStaleOnError(t *testing.T) {
	media := fixture(t, "media_182255.json")
	var hits atomic.Int32
	var fail atomic.Bool
	c, kv := server(t, func(w http.ResponseWriter, body string) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write(media)
	})
	ctx := context.Background()

	m, err := c.Media(ctx, 182255)
	if err != nil || m.IDMal != 59978 || m.AiredEpisodes() != 10 {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	if _, err := c.Media(ctx, 182255); err != nil || hits.Load() != 1 {
		t.Fatalf("fresh cache not used: hits=%d err=%v", hits.Load(), err)
	}

	// Expire the cache and break the API: stale data is still returned.
	kv.t["anilist:media:182255"] = time.Now().Add(-2 * mediaTTL)
	fail.Store(true)
	if m, err := c.Media(ctx, 182255); err != nil || m.ID != 182255 {
		t.Fatalf("stale fallback: m=%+v err=%v", m, err)
	}
}

func TestMediaNotFound(t *testing.T) {
	notFound := fixture(t, "media_not_found.json")
	c, _ := server(t, func(w http.ResponseWriter, body string) {
		w.WriteHeader(http.StatusNotFound)
		w.Write(notFound)
	})
	if _, err := c.Media(context.Background(), 999999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitRetry(t *testing.T) {
	media := fixture(t, "media_182255.json")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write(media)
	}))
	defer srv.Close()
	c := New(httpx.New(httpx.Options{}), nil)
	c.url = srv.URL

	start := time.Now()
	if _, err := c.Media(context.Background(), 182255); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("hits=%d elapsed=%v", hits.Load(), time.Since(start))
	}
}

func TestAiredEpisodes(t *testing.T) {
	airing := Media{Episodes: 24, Status: "RELEASING", NextAiringEpisode: &AiringEpisode{Episode: 8}}
	for _, tt := range []struct {
		m    Media
		want int
	}{
		{Media{Episodes: 28, Status: "FINISHED"}, 28},
		{airing, 7},
		{Media{Status: "NOT_YET_RELEASED", Episodes: 12}, 0},
		{Media{Status: "RELEASING"}, 0},
	} {
		if got := tt.m.AiredEpisodes(); got != tt.want {
			t.Errorf("%+v: got %d, want %d", tt.m, got, tt.want)
		}
	}
}

func TestTitlesDeduplicates(t *testing.T) {
	m := Media{Title: Title{English: "A", Romaji: "B", Native: "C"}, Synonyms: []string{"B", " ", "D"}}
	if got := strings.Join(m.Titles(), "|"); got != "A|B|C|D" {
		t.Fatalf("got %q", got)
	}
}
