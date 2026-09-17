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

	"github.com/EmoFa/tsuzuki/internal/httpx"
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

func TestAuthenticatedCalls(t *testing.T) {
	var auth []string
	c, _ := server(t, func(w http.ResponseWriter, body string) {}) // provides the kv cache
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		switch {
		case r.Header.Get("Authorization") != "Bearer tok":
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"errors":[{"message":"Invalid token","status":401}],"data":null}`)
		case strings.Contains(body, "Viewer"):
			io.WriteString(w, `{"data":{"Viewer":{"id":42,"name":"testuser","siteUrl":"https://anilist.co/user/testuser"}}}`)
		case strings.Contains(body, "SaveMediaListEntry"):
			if !strings.Contains(body, `"mediaId":182255`) || !strings.Contains(body, `"progress":5`) || !strings.Contains(body, `"status":"CURRENT"`) {
				t.Errorf("mutation variables: %s", body)
			}
			io.WriteString(w, `{"data":{"SaveMediaListEntry":{"id":1,"status":"CURRENT","progress":5}}}`)
		case strings.Contains(body, "MediaListCollection"):
			io.WriteString(w, `{"data":{"MediaListCollection":{"lists":[
				{"entries":[{"mediaId":182255,"status":"CURRENT","progress":5,"score":8.5,"updatedAt":1789400000,"media":{"id":182255,"title":{"english":"Frieren S2"}}}]},
				{"entries":[{"mediaId":182255,"status":"CURRENT","progress":5,"score":8.5,"updatedAt":1789400000,"media":{"id":182255}},
				            {"mediaId":154587,"status":"COMPLETED","progress":28,"score":0,"updatedAt":1789300000,"media":{"id":154587}}]}]}}}`)
		}
	}))
	defer srv.Close()
	c.url = srv.URL
	ctx := context.Background()

	if _, err := c.Viewer(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("anonymous viewer err = %v", err)
	}
	authed := c.WithToken("tok")
	u, err := authed.Viewer(ctx)
	if err != nil || u.ID != 42 || u.Name != "testuser" {
		t.Fatalf("viewer = %+v err=%v", u, err)
	}
	if err := authed.SaveListEntry(ctx, 182255, "CURRENT", 5, nil); err != nil {
		t.Fatal(err)
	}
	items, err := authed.UserList(ctx, 42)
	if err != nil || len(items) != 2 || items[0].Score != 8.5 || items[1].Status != "COMPLETED" || items[0].UpdatedAt.Unix() != 1789400000 {
		t.Fatalf("items = %+v err=%v", items, err)
	}
	// The untitled stub for 154587 must not have been cached.
	if _, _, ok, _ := authed.cache.GetKV(ctx, cacheKey(154587)); ok {
		t.Error("cached media details without a title")
	}
	if auth[0] != "" {
		t.Errorf("unauthenticated client sent %q", auth[0])
	}
}

func TestNotYetAiredAndPremiereLabel(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	future := now.Add(72 * time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()
	for _, tc := range []struct {
		name  string
		m     Media
		unair bool
		label string
	}{
		{"finished", Media{Status: "FINISHED"}, false, ""},
		{"airing", Media{Status: "RELEASING", NextAiringEpisode: &AiringEpisode{Episode: 4, AiringAt: future}}, false, ""},
		{"first episode scheduled", Media{Status: "NOT_YET_RELEASED", NextAiringEpisode: &AiringEpisode{Episode: 1, AiringAt: future}}, true, "Episode 1 airs Sun, Sep 20 at 12:00"},
		{"stale cache after premiere", Media{Status: "NOT_YET_RELEASED", NextAiringEpisode: &AiringEpisode{Episode: 1, AiringAt: past}}, false, ""},
		{"full start date", Media{Status: "NOT_YET_RELEASED", StartDate: FuzzyDate{2027, 1, 9}}, true, "Starts January 9, 2027"},
		{"month only", Media{Status: "NOT_YET_RELEASED", StartDate: FuzzyDate{Year: 2026, Month: 10}}, true, "Starts October 2026"},
		{"season", Media{Status: "NOT_YET_RELEASED", Season: "FALL", Year: 2026, StartDate: FuzzyDate{Year: 2026}}, true, "Expected Fall 2026"},
		{"year only", Media{Status: "NOT_YET_RELEASED", StartDate: FuzzyDate{Year: 2027}}, true, "Expected 2027"},
		{"unknown", Media{Status: "NOT_YET_RELEASED"}, true, "Release date not announced"},
	} {
		if got := tc.m.NotYetAired(); got != tc.unair {
			t.Errorf("%s: NotYetAired = %v", tc.name, got)
		}
		if tc.label != "" {
			if got := tc.m.PremiereLabel(now); got != tc.label {
				t.Errorf("%s: label = %q, want %q", tc.name, got, tc.label)
			}
		}
	}
}
