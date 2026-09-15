package skip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/httpx"
)

func TestFillerListKinds(t *testing.T) {
	index, _ := os.ReadFile("testdata/fillerlist_shows.html")
	show, _ := os.ReadFile("testdata/fillerlist_naruto_shippuden.html")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/shows":
			w.Write(index)
		case "/shows/naruto-shippuden":
			w.Write(show)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &FillerList{Client: httpx.New(httpx.Options{}), Cache: &memKV{m: map[string]string{}}, BaseURL: srv.URL}
	ctx := context.Background()

	shippuden := anilist.Media{ID: 1735, Title: anilist.Title{English: "Naruto: Shippuden", Romaji: "Naruto: Shippuuden"}, Synonyms: []string{"Naruto Shippuden"}}
	kinds, err := f.Kinds(ctx, shippuden)
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 500 || kinds[1] != Mixed {
		t.Fatalf("got %d kinds, ep1=%q", len(kinds), kinds[1])
	}
	counts := map[EpisodeKind]int{}
	for _, k := range kinds {
		counts[k]++
	}
	if counts[Filler] == 0 || counts[Canon] == 0 {
		t.Fatalf("counts = %v", counts)
	}

	// Cached: repeating the lookup makes no requests.
	before := hits.Load()
	f.Kinds(ctx, shippuden)
	if hits.Load() != before {
		t.Fatalf("not cached: %d -> %d requests", before, hits.Load())
	}

	// A show not on the site, and a near-miss title, give nothing.
	for _, m := range []anilist.Media{
		{ID: 182255, Title: anilist.Title{English: "Frieren: Beyond Journey’s End Season 2"}},
		{ID: 2, Title: anilist.Title{English: "Naruto Shippuden: The Movie"}},
	} {
		if k, err := f.Kinds(ctx, m); err != nil || k != nil {
			t.Errorf("%s: kinds=%v err=%v", m.DisplayTitle(), k, err)
		}
	}
}

func TestMatchShowUsesParentheticalTitles(t *testing.T) {
	shows := []fillerShow{
		{"naruto", "Naruto"},
		{"bleach-thousand-year-blood-war", "Bleach: Thousand-Year Blood War (Bleach: Sennen Kessen-hen)"},
	}
	tybw := anilist.Media{Title: anilist.Title{Romaji: "Bleach: Sennen Kessen-hen", English: "BLEACH: Thousand-Year Blood War"}}
	if got := matchShow(shows, tybw); got != "bleach-thousand-year-blood-war" {
		t.Fatalf("got %q", got)
	}
	if got := matchShow(shows, anilist.Media{Title: anilist.Title{Romaji: "Boruto"}}); got != "" {
		t.Fatalf("got %q", got)
	}
}
