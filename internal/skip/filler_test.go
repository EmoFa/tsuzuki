package skip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/httpx"
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

func TestMatchShowPrefersTheShowsOwnTitle(t *testing.T) {
	// The index lists re-cuts before the show itself; their episode numbering
	// isn't the show's, so the bracketed name must not win.
	shows := []fillerShow{
		{"one-pace", "One Pace (One Piece)"},
		{"one-piece", "One Piece"},
	}
	onePiece := anilist.Media{Title: anilist.Title{English: "ONE PIECE", Romaji: "ONE PIECE"}}
	if got := matchShow(shows, onePiece); got != "one-piece" {
		t.Fatalf("got %q, want one-piece", got)
	}
	// A bracketed name is still used when nothing else matches.
	if got := matchShow(shows[:1], onePiece); got != "one-pace" {
		t.Fatalf("bracketed fallback: got %q", got)
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

func TestKindsIgnoresAListCoveringTooFewEpisodes(t *testing.T) {
	// A long-running show matched to a short entry: acting on it would skip
	// real episodes, so it's ignored.
	index := []byte(`<a href="/shows/naruto-shippuden">Naruto Shippuden</a>`)
	show := []byte(`<tr class="filler odd" id="eps-1"><tr class="manga_canon even" id="eps-2">`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shows" {
			w.Write(index)
			return
		}
		w.Write(show)
	}))
	defer srv.Close()

	f := &FillerList{Client: httpx.New(httpx.Options{}), BaseURL: srv.URL}
	long := anilist.Media{ID: 1735, Title: anilist.Title{English: "Naruto Shippuden"}, Episodes: 500, Status: "FINISHED"}
	kinds, err := f.Kinds(context.Background(), long)
	if err != nil || kinds != nil {
		t.Fatalf("kinds = %v, err = %v", kinds, err)
	}

	// A short show with the same list is fine.
	short := anilist.Media{ID: 2, Title: anilist.Title{English: "Naruto Shippuden"}, Episodes: 2, Status: "FINISHED"}
	if kinds, err = f.Kinds(context.Background(), short); err != nil || len(kinds) != 2 {
		t.Fatalf("short show: kinds = %v, err = %v", kinds, err)
	}
}
