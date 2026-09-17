package anilist

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// BrowseQuery filters and orders a page of anime. Empty fields don't filter.
type BrowseQuery struct {
	Sort       string // MediaSort, e.g. POPULARITY_DESC
	Season     string // WINTER, SPRING, SUMMER, FALL
	SeasonYear int
	Status     string // e.g. NOT_YET_RELEASED
	Genres     []string
	Formats    []string // TV, TV_SHORT, MOVIE, OVA, ONA, SPECIAL
	Page       int      // from 1
	PerPage    int
}

type BrowsePage struct {
	Media   []Media
	HasNext bool
}

// Browse returns one page of anime matching q (adult titles excluded).
func (c *Client) Browse(ctx context.Context, q BrowseQuery) (BrowsePage, error) {
	vars := map[string]any{"page": max(q.Page, 1), "perPage": q.PerPage}
	if q.PerPage <= 0 {
		vars["perPage"] = 25
	}
	// AniList treats a missing variable as no filter, so only set what's given.
	if q.Sort != "" {
		vars["sort"] = []string{q.Sort}
	}
	if q.Season != "" {
		vars["season"] = q.Season
	}
	if q.SeasonYear > 0 {
		vars["seasonYear"] = q.SeasonYear
	}
	if q.Status != "" {
		vars["status"] = q.Status
	}
	if len(q.Genres) > 0 {
		vars["genres"] = q.Genres
	}
	if len(q.Formats) > 0 {
		vars["formats"] = q.Formats
	}
	var data struct {
		Page struct {
			PageInfo struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	query := `query ($page: Int, $perPage: Int, $sort: [MediaSort], $season: MediaSeason, $seasonYear: Int,
		$status: MediaStatus, $genres: [String], $formats: [MediaFormat]) {
		Page(page: $page, perPage: $perPage) { pageInfo { hasNextPage }
		media(type: ANIME, isAdult: false, sort: $sort, season: $season, seasonYear: $seasonYear,
			status: $status, genre_in: $genres, format_in: $formats) { ` + mediaFields + ` } } }`
	if err := c.query(ctx, query, vars, &data); err != nil {
		return BrowsePage{}, fmt.Errorf("anilist browse: %w", err)
	}
	for _, m := range data.Page.Media {
		c.store(ctx, m)
	}
	return BrowsePage{Media: data.Page.Media, HasNext: data.Page.PageInfo.HasNextPage}, nil
}

const (
	genresKey = "anilist:genres"
	genresTTL = 7 * 24 * time.Hour
)

// Genres lists AniList's anime genres (adult ones excluded), cached for a week.
func (c *Client) Genres(ctx context.Context) ([]string, error) {
	if c.cache != nil {
		if v, updated, ok, err := c.cache.GetKV(ctx, genresKey); err == nil && ok && time.Since(updated) < genresTTL {
			var genres []string
			if json.Unmarshal([]byte(v), &genres) == nil && len(genres) > 0 {
				return genres, nil
			}
		}
	}
	var data struct {
		Genres []string `json:"GenreCollection"`
	}
	if err := c.query(ctx, `query { GenreCollection }`, nil, &data); err != nil {
		return nil, fmt.Errorf("anilist genres: %w", err)
	}
	genres := slices.DeleteFunc(data.Genres, func(g string) bool { return g == "Hentai" })
	if c.cache != nil {
		if b, err := json.Marshal(genres); err == nil {
			_ = c.cache.PutKV(ctx, genresKey, string(b))
		}
	}
	return genres, nil
}

// Formats are AniList's anime formats, for filtering.
var Formats = []string{"TV", "TV_SHORT", "MOVIE", "OVA", "ONA", "SPECIAL"}

// Seasons in calendar order: WINTER is January–March, FALL October–December.
var Seasons = []string{"WINTER", "SPRING", "SUMMER", "FALL"}

// SeasonOf returns the anime season containing t.
func SeasonOf(t time.Time) (season string, year int) {
	return Seasons[(int(t.Month())-1)/3], t.Year()
}

// NextSeason returns the season after season/year.
func NextSeason(season string, year int) (string, int) {
	i := slices.Index(Seasons, season) + 1
	if i == len(Seasons) {
		return Seasons[0], year + 1
	}
	return Seasons[i], year
}

// DiscoverList is a ready-made browse list.
type DiscoverList struct {
	Key   string // for the command line: season, trending, popular, top, upcoming
	Title string
	Query BrowseQuery
}

// DiscoverLists are the lists the Discover screen and command offer, for now.
func DiscoverLists(now time.Time) []DiscoverList {
	season, year := SeasonOf(now)
	nextSeason, nextYear := NextSeason(season, year)
	return []DiscoverList{
		{"season", "This season", BrowseQuery{Sort: "POPULARITY_DESC", Season: season, SeasonYear: year}},
		{"trending", "Trending", BrowseQuery{Sort: "TRENDING_DESC"}},
		{"popular", "Popular", BrowseQuery{Sort: "POPULARITY_DESC"}},
		{"top", "Top rated", BrowseQuery{Sort: "SCORE_DESC"}},
		{"upcoming", "Next season", BrowseQuery{Sort: "POPULARITY_DESC", Season: nextSeason, SeasonYear: nextYear}},
	}
}
