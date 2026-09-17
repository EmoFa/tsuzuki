package anilist

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestBrowse(t *testing.T) {
	var vars map[string]any
	c, kv := server(t, func(w http.ResponseWriter, body string) {
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		json.Unmarshal([]byte(body), &req)
		vars = req.Variables
		w.Write(fixture(t, "browse_season.json"))
	})
	ctx := context.Background()

	page, err := c.Browse(ctx, BrowseQuery{Sort: "POPULARITY_DESC", Season: "SUMMER", SeasonYear: 2026, Genres: []string{"Action"}, Page: 2, PerPage: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Media) != 3 || !page.HasNext || page.Media[0].ID != 178789 || page.Media[0].Season != "SUMMER" {
		t.Fatalf("page = %+v", page)
	}
	if vars["page"] != float64(2) || vars["season"] != "SUMMER" || vars["seasonYear"] != float64(2026) {
		t.Errorf("variables = %v", vars)
	}
	for _, unset := range []string{"status", "formats"} {
		if _, ok := vars[unset]; ok {
			t.Errorf("empty filter %q sent: %v", unset, vars)
		}
	}
	if _, _, ok, _ := kv.GetKV(ctx, cacheKey(178789)); !ok {
		t.Error("results not cached")
	}

	// Defaults: page 1, 25 per page, no filters.
	if _, err := c.Browse(ctx, BrowseQuery{}); err != nil {
		t.Fatal(err)
	}
	if vars["page"] != float64(1) || vars["perPage"] != float64(25) || len(vars) != 2 {
		t.Errorf("default variables = %v", vars)
	}
}

func TestGenresCachedWithoutAdult(t *testing.T) {
	calls := 0
	c, _ := server(t, func(w http.ResponseWriter, body string) {
		calls++
		w.Write([]byte(`{"data":{"GenreCollection":["Action","Hentai","Romance"]}}`))
	})
	for range 2 {
		genres, err := c.Genres(context.Background())
		if err != nil || !slices.Equal(genres, []string{"Action", "Romance"}) {
			t.Fatalf("genres = %v, %v", genres, err)
		}
	}
	if calls != 1 {
		t.Errorf("fetched %d times, want 1", calls)
	}
}

func TestSeasons(t *testing.T) {
	for _, tc := range []struct {
		month      time.Month
		season     string
		next       string
		nextYearUp bool
	}{
		{time.January, "WINTER", "SPRING", false},
		{time.March, "WINTER", "SPRING", false},
		{time.April, "SPRING", "SUMMER", false},
		{time.September, "SUMMER", "FALL", false},
		{time.December, "FALL", "WINTER", true},
	} {
		s, y := SeasonOf(time.Date(2026, tc.month, 15, 0, 0, 0, 0, time.UTC))
		ns, ny := NextSeason(s, y)
		if s != tc.season || y != 2026 || ns != tc.next || (ny == 2027) != tc.nextYearUp {
			t.Errorf("%s: %s %d → %s %d", tc.month, s, y, ns, ny)
		}
	}
	lists := DiscoverLists(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if lists[0].Query.Season != "FALL" || lists[4].Query.Season != "WINTER" || lists[4].Query.SeasonYear != 2027 {
		t.Errorf("lists = %+v", lists)
	}
}
