package anilist

import (
	"context"
	"encoding/json"
	"fmt"
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
var episodeTitle = regexp.MustCompile(`^(?:Episode|Ep\.?|E)\s*(\d+(?:\.\d+)?)\s*(?:[-–—:]\s*(.*))?$`)

// StreamingEpisodes returns the episodes AniList lists from streaming sites,
// by episode number. Many shows have none; that's an empty map, not an error.
func (c *Client) StreamingEpisodes(ctx context.Context, id int) (map[float64]EpisodeInfo, error) {
	key := "anilist:episodes:" + strconv.Itoa(id)
	if c.cache != nil {
		if v, updated, ok, err := c.cache.GetKV(ctx, key); err == nil && ok && time.Since(updated) < episodesTTL {
			var eps map[float64]EpisodeInfo
			if json.Unmarshal([]byte(v), &eps) == nil {
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
		if b, err := json.Marshal(eps); err == nil {
			_ = c.cache.PutKV(ctx, key, string(b))
		}
	}
	return eps, nil
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
