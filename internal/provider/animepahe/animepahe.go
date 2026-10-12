// Package animepahe implements the Animepahe provider. See docs/providers/animepahe.md.
package animepahe

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/extractor"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider"
)

const (
	Name           = "animepahe"
	DefaultBaseURL = "https://animepahe.pw"
	// maxReleasePages guards against a misbehaving API paginating forever.
	maxReleasePages = 200
)

var (
	buttonTag   = regexp.MustCompile(`<button[^>]*\bdata-src="[^"]*"[^>]*>`)
	htmlAttr    = regexp.MustCompile(`([\w-]+)="([^"]*)"`)
	anilistMeta = regexp.MustCompile(`<meta name="anilist" content="(\d+)"`)
	malLink     = regexp.MustCompile(`myanimelist\.net/anime/(\d+)`)
)

type Provider struct {
	client *httpx.Client
	base   string
}

func New(client *httpx.Client, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Provider{client: client, base: strings.TrimRight(baseURL, "/")}
}

func (p *Provider) Name() string { return Name }

func (p *Provider) headers() map[string]string {
	return map[string]string{"Referer": p.base + "/"}
}

type searchResponse struct {
	Data []struct {
		ID       int     `json:"id"`
		Title    string  `json:"title"`
		Type     string  `json:"type"`
		Episodes int     `json:"episodes"`
		Year     int     `json:"year"`
		Poster   string  `json:"poster"`
		Session  string  `json:"session"`
		Score    float64 `json:"score"`
	} `json:"data"`
}

// Search ignores mode: Animepahe lists sub and dub under the same show.
func (p *Provider) Search(ctx context.Context, query string, _ domain.Mode) ([]domain.Show, error) {
	var resp searchResponse
	u := p.base + "/api?m=search&q=" + url.QueryEscape(query)
	if err := p.client.GetJSON(ctx, u, p.headers(), &resp); err != nil {
		return nil, fmt.Errorf("animepahe search: %w", err)
	}
	shows := make([]domain.Show, 0, len(resp.Data))
	for _, d := range resp.Data {
		shows = append(shows, domain.Show{
			Provider: Name,
			ID:       d.Session,
			Title:    html.UnescapeString(d.Title),
			Type:     d.Type,
			Year:     d.Year,
			Episodes: d.Episodes,
			Poster:   d.Poster,
		})
	}
	return shows, nil
}

type releaseResponse struct {
	LastPage int `json:"last_page"`
	Data     []struct {
		Episode  float64 `json:"episode"`
		Title    string  `json:"title"`
		Duration string  `json:"duration"`
		Filler   int     `json:"filler"`
		Session  string  `json:"session"`
	} `json:"data"`
}

func (p *Provider) Episodes(ctx context.Context, showID string, _ domain.Mode) ([]domain.Episode, error) {
	var eps []domain.Episode
	for page := 1; page <= maxReleasePages; page++ {
		var resp releaseResponse
		u := fmt.Sprintf("%s/api?m=release&id=%s&sort=episode_asc&page=%d", p.base, url.QueryEscape(showID), page)
		if err := p.client.GetJSON(ctx, u, p.headers(), &resp); err != nil {
			return nil, fmt.Errorf("animepahe episodes: %w", err)
		}
		for _, d := range resp.Data {
			eps = append(eps, domain.Episode{
				ID:           d.Session,
				Number:       d.Episode,
				SourceNumber: strconv.FormatFloat(d.Episode, 'f', -1, 64),
				Title:        html.UnescapeString(d.Title),
				Filler:       d.Filler == 1,
				Duration:     parseDuration(d.Duration),
			})
		}
		if page >= resp.LastPage {
			break
		}
	}
	if len(eps) == 0 {
		return nil, fmt.Errorf("animepahe show %s: %w", showID, provider.ErrNotFound)
	}
	return renumber(eps), nil
}

// renumber sorts episodes and shifts numbering to start at 1 when the provider
// continues counting from a previous season (e.g. season 2 listed as 29–38).
func renumber(eps []domain.Episode) []domain.Episode {
	slices.SortStableFunc(eps, func(a, b domain.Episode) int {
		switch {
		case a.Number < b.Number:
			return -1
		case a.Number > b.Number:
			return 1
		}
		return 0
	})
	if first := eps[0].Number; first > 1 {
		offset := float64(int(first) - 1)
		for i := range eps {
			eps[i].Number -= offset
		}
	}
	return eps
}

type choice struct {
	embed  string
	fansub string
	height int
	audio  string
	av1    bool
}

func (p *Provider) Streams(ctx context.Context, showID string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error) {
	page, err := p.client.Get(ctx, fmt.Sprintf("%s/play/%s/%s", p.base, showID, ep.ID), p.headers())
	if err != nil {
		return nil, fmt.Errorf("animepahe play page: %w", err)
	}
	choices := parseChoices(string(page))
	want := "jpn"
	if mode == domain.Dub {
		want = "eng"
	}
	choices = slices.DeleteFunc(choices, func(c choice) bool { return c.audio != want })
	if len(choices) == 0 {
		return nil, fmt.Errorf("animepahe episode %s (%s): %w", ep.Label(), mode, provider.ErrNoStreams)
	}

	type result struct {
		stream domain.Stream
		err    error
	}
	results := make([]result, len(choices))
	var wg sync.WaitGroup
	for i, c := range choices {
		wg.Go(func() {
			src, err := extractor.ResolveKwik(ctx, p.client, c.embed, p.base+"/")
			if err != nil {
				results[i].err = err
				return
			}
			label := fmt.Sprintf("%s · %dp", c.fansub, c.height)
			if c.av1 {
				label += " AV1"
			}
			results[i].stream = domain.Stream{
				URL:        src,
				Kind:       domain.HLS,
				Height:     c.height,
				Audio:      mode,
				Label:      label,
				Headers:    map[string]string{"Referer": origin(c.embed)},
				NeedsProxy: true,
				Playlist:   playlistIVs,
			}
		})
	}
	wg.Wait()

	var streams []domain.Stream
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		streams = append(streams, r.stream)
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("animepahe: resolving embeds: %w", errors.Join(errs...))
	}
	if len(errs) > 0 {
		slog.Warn("animepahe: some embeds failed", "episode", ep.Label(), "failed", len(errs), "err", errors.Join(errs...))
	}
	return streams, nil
}

func parseChoices(page string) []choice {
	var out []choice
	for _, tag := range buttonTag.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, m := range htmlAttr.FindAllStringSubmatch(tag, -1) {
			attrs[m[1]] = html.UnescapeString(m[2])
		}
		height, _ := strconv.Atoi(attrs["data-resolution"])
		out = append(out, choice{
			embed:  attrs["data-src"],
			fansub: attrs["data-fansub"],
			height: height,
			audio:  attrs["data-audio"],
			av1:    attrs["data-av1"] == "1",
		})
	}
	return out
}

func (p *Provider) ExternalIDs(ctx context.Context, showID string) (anilistID, malID int, err error) {
	page, err := p.client.Get(ctx, p.base+"/anime/"+showID, p.headers())
	if err != nil {
		return 0, 0, fmt.Errorf("animepahe anime page: %w", err)
	}
	if m := anilistMeta.FindSubmatch(page); m != nil {
		anilistID, _ = strconv.Atoi(string(m[1]))
	}
	if m := malLink.FindSubmatch(page); m != nil {
		malID, _ = strconv.Atoi(string(m[1]))
	}
	return anilistID, malID, nil
}

// parseDuration reads "hh:mm:ss"; malformed values yield 0.
func parseDuration(s string) time.Duration {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	var total time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0
		}
		total += time.Duration(n) * unit
	}
	return total
}

// origin returns "scheme://host/" for a URL.
func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}

// kwikSegment matches the file name of kwik's segments, numbered from 1.
var kwikSegment = regexp.MustCompile(`segment-(\d+)-[^/]*$`)

// playlistIVs is the codec kwik's playlists need. They're AES-128 encrypted
// with no IV given, which per HLS means each segment's media sequence number,
// but kwik encrypts with the number in the segment's file name, and some
// playlists skip numbers. Past a gap, a player derives the wrong IV, which
// garbles each segment's first 16 bytes: harmless mid-playback, but starting
// or seeking there loses the stream map, and ExoPlayer never finds the video.
var playlistIVs = &domain.PlaylistCodec{Prefix: []byte("#EXTM3U"), Decode: explicitIVs}

// explicitIVs states each encrypted segment's IV before it, from its file name.
func explicitIVs(body []byte) ([]byte, error) {
	lines := strings.Split(string(body), "\n")
	out := make([]string, 0, 2*len(lines))
	key := "" // the EXT-X-KEY in force, when it gives no IV
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "#EXT-X-KEY:"):
			key = ""
			if strings.Contains(t, "METHOD=AES-128") && !strings.Contains(t, "IV=") {
				key = t
			}
		case key != "" && t != "" && !strings.HasPrefix(t, "#"):
			name, _, _ := strings.Cut(t, "?")
			if m := kwikSegment.FindStringSubmatch(name); m != nil {
				n, err := strconv.Atoi(m[1])
				if err == nil {
					out = append(out, fmt.Sprintf("%s,IV=0x%032x", key, n))
				}
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n")), nil
}
