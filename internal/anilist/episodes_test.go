package anilist

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/httpx"
)

func TestParseEpisodeTitle(t *testing.T) {
	for _, tc := range []struct {
		in    string
		n     float64
		title string
		ok    bool
	}{
		{"Episode 3 - Killing Magic", 3, "Killing Magic", true},
		{"Episode 12: The Real Hero", 12, "The Real Hero", true},
		{"E7 - Hard", 7, "Hard", true},
		{"Episode 13.5 - Recap", 13.5, "Recap", true},
		{"Episode 4", 4, "", true},
		{"Frieren Special", 0, "", false},
		{"episode 5 - lowercase", 5, "lowercase", true},
		{"EP07: Colon", 7, "Colon", true},
		{"Your Name.", 0, "", false},
	} {
		n, title, ok := parseEpisodeTitle(tc.in)
		if n != tc.n || title != tc.title || ok != tc.ok {
			t.Errorf("%q: %v %q %v", tc.in, n, title, ok)
		}
	}
}

func TestStreamingEpisodes(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"data":{"Media":{"streamingEpisodes":[
			{"title":"Episode 2 - It Didn't Have to Be Magic...","thumbnail":"https://img/2.jpg"},
			{"title":"Episode 1 - The Journey's End","thumbnail":"https://img/1.jpg"},
			{"title":"Episode 1 - Another site","thumbnail":"https://img/other.jpg"},
			{"title":"Trailer","thumbnail":"https://img/t.jpg"}]}}}`))
	}))
	defer srv.Close()
	c := New(httpx.New(httpx.Options{Retries: -1}), newMemKV()).WithURL(srv.URL)
	for range 2 {
		eps, err := c.StreamingEpisodes(t.Context(), 154587)
		if err != nil {
			t.Fatal(err)
		}
		if len(eps) != 2 || eps[1].Title != "The Journey's End" || eps[1].Thumbnail != "https://img/1.jpg" || eps[2].Thumbnail == "" {
			t.Fatalf("episodes %+v", eps)
		}
	}
	if hits != 1 {
		t.Errorf("asked AniList %d times; the second should come from the cache", hits)
	}
}

func TestAlignEpisodes(t *testing.T) {
	mk := func(ns ...float64) map[float64]EpisodeInfo {
		m := map[float64]EpisodeInfo{}
		for _, n := range ns {
			m[n] = EpisodeInfo{Title: "t"}
		}
		return m
	}
	keys := func(m map[float64]EpisodeInfo) (ks []float64) {
		for n := range m {
			ks = append(ks, n)
		}
		slices.Sort(ks)
		return
	}
	for _, tc := range []struct {
		name  string
		in    map[float64]EpisodeInfo
		total int
		want  []float64
	}{
		{"absolute", mk(13, 14, 15), 12, []float64{1, 2, 3}},
		{"already aligned", mk(1, 2, 3), 12, []float64{1, 2, 3}},
		{"unknown total", mk(13, 14), 0, []float64{13, 14}},
		{"gap", mk(13, 15), 12, []float64{13, 15}},
		{"overlaps season", mk(10, 11, 12, 13), 12, []float64{10, 11, 12, 13}},
		{"longer than season", mk(139, 140, 141, 142), 3, []float64{139, 140, 141, 142}},
		{"fractional", mk(13, 13.5), 12, []float64{13, 13.5}},
	} {
		if got := keys(AlignEpisodes(tc.in, tc.total)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
