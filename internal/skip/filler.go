package skip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/mapping"
)

const (
	DefaultFillerListURL = "https://www.animefillerlist.com"
	fillerTTL            = 7 * 24 * time.Hour
)

// EpisodeKind classifies an episode's source material.
type EpisodeKind string

const (
	Canon      EpisodeKind = "canon"       // adapts the manga
	AnimeCanon EpisodeKind = "anime canon" // original but canonical
	Mixed      EpisodeKind = "mixed"       // canon and filler
	Filler     EpisodeKind = "filler"
)

var (
	showLink   = regexp.MustCompile(`<a href="/shows/([a-z0-9-]+)">([^<]+)</a>`)
	episodeRow = regexp.MustCompile(`<tr class="([a-z_/]+) (?:odd|even)" id="eps-(\d+)">`)
	altTitle   = regexp.MustCompile(`^(.*?)\s*\((.+)\)\s*$`)
)

// FillerList reads animefillerlist.com.
type FillerList struct {
	Client  *httpx.Client
	Cache   Cache // optional
	BaseURL string
}

type fillerShow struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Kinds returns episode kinds by episode number, or nil when the site doesn't
// list the show. Only an exact title match is used: a wrong show would skip
// real episodes.
func (f *FillerList) Kinds(ctx context.Context, media anilist.Media) (map[int]EpisodeKind, error) {
	shows, err := f.index(ctx)
	if err != nil {
		return nil, err
	}
	slug := matchShow(shows, media)
	if slug == "" {
		return nil, nil
	}
	return f.show(ctx, slug)
}

func matchShow(shows []fillerShow, media anilist.Media) string {
	want := map[string]bool{}
	for _, t := range media.Titles() {
		if n := mapping.NormalizeTitle(t); n != "" {
			want[n] = true
		}
	}
	for _, s := range shows {
		names := []string{s.Title}
		// "Bleach: Thousand-Year Blood War (Bleach: Sennen Kessen-hen)"
		if m := altTitle.FindStringSubmatch(s.Title); m != nil {
			names = []string{m[1], m[2]}
		}
		for _, n := range names {
			if want[mapping.NormalizeTitle(n)] {
				return s.Slug
			}
		}
	}
	return ""
}

func (f *FillerList) index(ctx context.Context) ([]fillerShow, error) {
	var shows []fillerShow
	if f.cached(ctx, "fillerlist:index", &shows) {
		return shows, nil
	}
	page, err := f.Client.Get(ctx, f.base()+"/shows", nil)
	if err != nil {
		return nil, fmt.Errorf("animefillerlist: %w", err)
	}
	seen := map[string]bool{}
	for _, m := range showLink.FindAllStringSubmatch(string(page), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			shows = append(shows, fillerShow{Slug: m[1], Title: html.UnescapeString(strings.TrimSpace(m[2]))})
		}
	}
	if len(shows) == 0 {
		return nil, errors.New("animefillerlist: show index is empty (page layout changed?)")
	}
	f.store(ctx, "fillerlist:index", shows)
	return shows, nil
}

func (f *FillerList) show(ctx context.Context, slug string) (map[int]EpisodeKind, error) {
	key := "fillerlist:show:" + slug
	var kinds map[int]EpisodeKind
	if f.cached(ctx, key, &kinds) {
		return kinds, nil
	}
	page, err := f.Client.Get(ctx, f.base()+"/shows/"+slug, nil)
	if err != nil {
		return nil, fmt.Errorf("animefillerlist %s: %w", slug, err)
	}
	kinds = parseEpisodeKinds(string(page))
	f.store(ctx, key, kinds)
	return kinds, nil
}

func parseEpisodeKinds(page string) map[int]EpisodeKind {
	kinds := map[int]EpisodeKind{}
	for _, m := range episodeRow.FindAllStringSubmatch(page, -1) {
		n, _ := strconv.Atoi(m[2])
		switch m[1] {
		case "manga_canon":
			kinds[n] = Canon
		case "anime_canon":
			kinds[n] = AnimeCanon
		case "mixed_canon/filler":
			kinds[n] = Mixed
		case "filler":
			kinds[n] = Filler
		}
	}
	return kinds
}

func (f *FillerList) base() string {
	if f.BaseURL != "" {
		return f.BaseURL
	}
	return DefaultFillerListURL
}

func (f *FillerList) cached(ctx context.Context, key string, v any) bool {
	if f.Cache == nil {
		return false
	}
	s, updated, ok, err := f.Cache.GetKV(ctx, key)
	return err == nil && ok && time.Since(updated) < fillerTTL && json.Unmarshal([]byte(s), v) == nil
}

func (f *FillerList) store(ctx context.Context, key string, v any) {
	if f.Cache == nil {
		return
	}
	data, err := json.Marshal(v)
	if err == nil {
		err = f.Cache.PutKV(ctx, key, string(data))
	}
	if err != nil {
		slog.Warn("animefillerlist: caching", "key", key, "err", err)
	}
}
