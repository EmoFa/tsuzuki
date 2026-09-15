package skip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
)

type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) GetKV(_ context.Context, key string) (string, time.Time, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	return v, time.Now(), ok, nil
}

func (k *memKV) PutKV(_ context.Context, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = value
	return nil
}

func TestAniSkipRangesAndCache(t *testing.T) {
	found, _ := os.ReadFile("testdata/aniskip_59978_1.json")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.Contains(r.URL.RawQuery, "types=op") || !strings.Contains(r.URL.RawQuery, "types=recap") {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		switch r.URL.Path {
		case "/skip-times/59978/1":
			w.Write(found)
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"found":false,"results":[],"message":"No skip times found","statusCode":404}`))
		}
	}))
	defer srv.Close()
	a := &AniSkip{Client: httpx.New(httpx.Options{}), Cache: &memKV{m: map[string]string{}}, BaseURL: srv.URL}
	ctx := context.Background()

	got, err := a.Ranges(ctx, 59978, 1, 24*time.Minute)
	want := domain.SkipRange{Kind: domain.SkipOpening, Start: 117 * time.Second, End: 207 * time.Second}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v err=%v", got, err)
	}
	// Cached: no second request. A different cut of the episode gets nothing.
	if got, _ := a.Ranges(ctx, 59978, 1, 20*time.Minute); len(got) != 0 {
		t.Fatalf("mismatched length kept ranges: %+v", got)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want cached", hits.Load())
	}

	if got, err := a.Ranges(ctx, 59978, 2, 0); err != nil || len(got) != 0 {
		t.Fatalf("not found: %+v err=%v", got, err)
	}
	if got, _ := a.Ranges(ctx, 0, 1, 0); got != nil {
		t.Fatal("no MAL ID should not query")
	}
}

func TestSkipperAuto(t *testing.T) {
	s := NewSkipper(map[domain.SkipKind]string{domain.SkipOpening: Auto, domain.SkipEnding: Auto, domain.SkipRecap: Off})
	s.SetRanges([]domain.SkipRange{
		{Kind: domain.SkipEnding, Start: 1340 * time.Second, End: 1430 * time.Second},
		{Kind: domain.SkipOpening, Start: 117 * time.Second, End: 207 * time.Second},
		{Kind: domain.SkipRecap, Start: 0, End: 60 * time.Second},                // off
		{Kind: domain.SkipOpening, Start: 5 * time.Second, End: 5 * time.Second}, // invalid
	})
	if d := s.Position(30 * time.Second); !d.None() {
		t.Fatalf("recap is off but got %+v", d)
	}
	if d := s.Position(118 * time.Second); d.Seek != 207*time.Second || d.Kind != domain.SkipOpening {
		t.Fatalf("opening: %+v", d)
	}
	// Seeking back into the opening is respected.
	if d := s.Position(150 * time.Second); !d.None() {
		t.Fatalf("re-skipped after seeking back: %+v", d)
	}
	// Too close to the end to bother.
	if d := s.Position(1429 * time.Second); !d.None() {
		t.Fatalf("near end of range: %+v", d)
	}
	if d := s.Position(1341 * time.Second); d.Seek != 1430*time.Second {
		t.Fatalf("ending: %+v", d)
	}
}

func TestSkipperPromptAndRequest(t *testing.T) {
	s := NewSkipper(map[domain.SkipKind]string{domain.SkipOpening: Prompt})
	op := domain.SkipRange{Kind: domain.SkipOpening, Start: 100 * time.Second, End: 190 * time.Second}
	s.SetRanges([]domain.SkipRange{op})

	d := s.Position(101 * time.Second)
	if !d.Prompt || d.Seek != 0 || d.Until != 89*time.Second {
		t.Fatalf("prompt: %+v", d)
	}
	if d := s.Position(102 * time.Second); !d.None() {
		t.Fatalf("prompted twice: %+v", d)
	}
	if d := s.Request(120 * time.Second); d.Seek != 190*time.Second {
		t.Fatalf("request: %+v", d)
	}
	if d := s.Request(10 * time.Second); !d.None() {
		t.Fatalf("request outside a range: %+v", d)
	}
	// Re-setting the same ranges (e.g. AniSkip arriving later) keeps state.
	s.SetRanges([]domain.SkipRange{op})
	if d := s.Position(105 * time.Second); !d.None() {
		t.Fatalf("state lost on SetRanges: %+v", d)
	}
}
