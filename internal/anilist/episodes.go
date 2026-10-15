package anilist

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// episodesTTL: airing shows gain episodes, so their list is refreshed daily.
const episodesTTL = 24 * time.Hour

// EpisodeInfo is what AniList knows of one episode from its streaming
// listings: a title and a thumbnail.
type EpisodeInfo struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
}

// episodeTitle reads "Episode 3 - Killing Magic" (also "E3 - …" and "Episode 3: …").
var episodeTitle = regexp.MustCompile(`(?i)^(?:Episode|Ep\.?|E)\s*(\d+(?:\.\d+)?)\s*(?:[-–—:]\s*(.*))?$`)

// StreamingEpisodes returns the episodes AniList lists from streaming sites,
// by episode number. Many shows have none; that's an empty map, not an error.
func (c *Client) StreamingEpisodes(ctx context.Context, id int) (map[float64]EpisodeInfo, error) {
	key := "anilist:episodes:" + strconv.Itoa(id)
	if c.cache != nil {
		if v, updated, ok, err := c.cache.GetKV(ctx, key); err == nil && ok && time.Since(updated) < episodesTTL {
			var cached []cachedEpisode
			if json.Unmarshal([]byte(v), &cached) == nil {
				eps := make(map[float64]EpisodeInfo, len(cached))
				for _, e := range cached {
					eps[e.Number] = e.EpisodeInfo
				}
				return eps, nil
			}
		}
	}
	var data struct {
		Media *struct {
			StreamingEpisodes []struct {
				Title     string `json:"title"`
				Thumbnail string `json:"thumbnail"`
			} `json:"streamingEpisodes"`
		} `json:"Media"`
	}
	q := `query ($id: Int) { Media(id: $id, type: ANIME) { streamingEpisodes { title thumbnail } } }`
	if err := c.query(ctx, q, map[string]any{"id": id}, &data); err != nil {
		return nil, fmt.Errorf("anilist episodes %d: %w", id, err)
	}
	if data.Media == nil {
		return nil, fmt.Errorf("anilist episodes %d: %w", id, ErrNotFound)
	}
	eps := map[float64]EpisodeInfo{}
	for _, e := range data.Media.StreamingEpisodes {
		n, title, ok := parseEpisodeTitle(e.Title)
		if !ok {
			continue
		}
		if _, seen := eps[n]; !seen { // the first site listed wins
			eps[n] = EpisodeInfo{Title: title, Thumbnail: e.Thumbnail}
		}
	}
	if c.cache != nil {
		// A list, since encoding/json before Go 1.27 can't key a map by float.
		cached := make([]cachedEpisode, 0, len(eps))
		for n, e := range eps {
			cached = append(cached, cachedEpisode{n, e})
		}
		if b, err := json.Marshal(cached); err == nil {
			_ = c.cache.PutKV(ctx, key, string(b))
		}
	}
	return eps, nil
}

// AlignEpisodes shifts listings that continue an earlier season's numbering
// (a 12-episode season listed as 13-24) back to start at 1. It acts only when
// the season's length is known and the numbers are whole, consecutive, and
// entirely beyond it, so a listing it can't place is left as it is.
func AlignEpisodes(eps map[float64]EpisodeInfo, total int) map[float64]EpisodeInfo {
	if total <= 0 || len(eps) == 0 {
		return eps
	}
	lo, hi := math.Inf(1), 0.0
	for n := range eps {
		if n != math.Trunc(n) {
			return eps
		}
		lo, hi = min(lo, n), max(hi, n)
	}
	if lo <= float64(total) || hi-lo+1 != float64(len(eps)) || len(eps) > total {
		return eps
	}
	out := make(map[float64]EpisodeInfo, len(eps))
	for n, e := range eps {
		out[n-lo+1] = e
	}
	return out
}

// cachedEpisode is one entry of a cached StreamingEpisodes result.
type cachedEpisode struct {
	Number float64 `json:"number"`
	EpisodeInfo
}

// parseEpisodeTitle splits "Episode 3 - Killing Magic" into 3 and "Killing Magic".
func parseEpisodeTitle(s string) (number float64, title string, ok bool) {
	m := episodeTitle.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, "", false
	}
	return n, strings.TrimSpace(m[2]), true
}
