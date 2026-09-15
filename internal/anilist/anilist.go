// Package anilist is a client for AniList's public GraphQL API. AniList media
// IDs are anitui's canonical anime identity.
package anilist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/EmoFa/anitui/internal/httpx"
)

const DefaultURL = "https://graphql.anilist.co"

// mediaTTL is how long cached media details are trusted. AniList rate limits
// aggressively (30-90 requests/minute), so details are cached.
const mediaTTL = 12 * time.Hour

var ErrNotFound = errors.New("anilist: not found")

const mediaFields = `id idMal title { romaji english native } synonyms format status episodes duration
season seasonYear coverImage { extraLarge large color } bannerImage description(asHtml: false)
averageScore genres isAdult nextAiringEpisode { episode airingAt }`

type Media struct {
	ID       int      `json:"id"`
	IDMal    int      `json:"idMal"`
	Title    Title    `json:"title"`
	Synonyms []string `json:"synonyms"`
	Format   string   `json:"format"` // TV, MOVIE, ONA, ...
	Status   string   `json:"status"` // FINISHED, RELEASING, NOT_YET_RELEASED, ...
	Episodes int      `json:"episodes"`
	Duration int      `json:"duration"` // minutes per episode
	Season   string   `json:"season"`
	Year     int      `json:"seasonYear"`
	Cover    struct {
		ExtraLarge string `json:"extraLarge"`
		Large      string `json:"large"`
		Color      string `json:"color"`
	} `json:"coverImage"`
	Banner            string         `json:"bannerImage"`
	Description       string         `json:"description"`
	AverageScore      int            `json:"averageScore"`
	Genres            []string       `json:"genres"`
	IsAdult           bool           `json:"isAdult"`
	NextAiringEpisode *AiringEpisode `json:"nextAiringEpisode"`
}

type AiringEpisode struct {
	Episode  int   `json:"episode"`
	AiringAt int64 `json:"airingAt"` // unix seconds
}

type Title struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

// DisplayTitle prefers the English title.
func (m Media) DisplayTitle() string {
	if m.Title.English != "" {
		return m.Title.English
	}
	return m.Title.Romaji
}

// Titles lists every known title, most recognisable first, without duplicates.
func (m Media) Titles() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append([]string{m.Title.English, m.Title.Romaji, m.Title.Native}, m.Synonyms...) {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// AiredEpisodes is how many episodes exist so far, or 0 when unknown.
func (m Media) AiredEpisodes() int {
	if m.NextAiringEpisode != nil {
		return m.NextAiringEpisode.Episode - 1
	}
	if m.Status == "NOT_YET_RELEASED" {
		return 0
	}
	return m.Episodes
}

// Cache stores small values by key; the store's kv table implements it.
type Cache interface {
	GetKV(ctx context.Context, key string) (value string, updated time.Time, ok bool, err error)
	PutKV(ctx context.Context, key, value string) error
}

type Client struct {
	http  *httpx.Client
	url   string
	cache Cache // optional
}

func New(client *httpx.Client, cache Cache) *Client {
	return &Client{http: client, url: DefaultURL, cache: cache}
}

// Search finds anime (adult titles excluded) in AniList's relevance order.
func (c *Client) Search(ctx context.Context, query string, perPage int) ([]Media, error) {
	var data struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	q := `query ($search: String, $perPage: Int) { Page(page: 1, perPage: $perPage) {
		media(search: $search, type: ANIME, sort: SEARCH_MATCH, isAdult: false) { ` + mediaFields + ` } } }`
	if err := c.query(ctx, q, map[string]any{"search": query, "perPage": perPage}, &data); err != nil {
		return nil, fmt.Errorf("anilist search: %w", err)
	}
	for _, m := range data.Page.Media {
		c.store(ctx, m)
	}
	return data.Page.Media, nil
}

// Media returns details for id, from cache when fresh. If AniList can't be
// reached, stale cached details are returned rather than failing.
func (c *Client) Media(ctx context.Context, id int) (Media, error) {
	cached, fresh, haveCached := c.cached(ctx, id)
	if haveCached && fresh {
		return cached, nil
	}
	var data struct {
		Media *Media `json:"Media"`
	}
	q := `query ($id: Int) { Media(id: $id, type: ANIME) { ` + mediaFields + ` } }`
	err := c.query(ctx, q, map[string]any{"id": id}, &data)
	if err == nil && data.Media == nil {
		err = ErrNotFound
	}
	if err != nil {
		if haveCached && !errors.Is(err, ErrNotFound) {
			slog.Warn("anilist unavailable; using stale cache", "id", id, "err", err)
			return cached, nil
		}
		return Media{}, fmt.Errorf("anilist media %d: %w", id, err)
	}
	c.store(ctx, *data.Media)
	return *data.Media, nil
}

func cacheKey(id int) string { return "anilist:media:" + strconv.Itoa(id) }

func (c *Client) cached(ctx context.Context, id int) (m Media, fresh, ok bool) {
	if c.cache == nil {
		return m, false, false
	}
	value, updated, ok, err := c.cache.GetKV(ctx, cacheKey(id))
	if err != nil || !ok || json.Unmarshal([]byte(value), &m) != nil {
		return m, false, false
	}
	return m, time.Since(updated) < mediaTTL, true
}

func (c *Client) store(ctx context.Context, m Media) {
	if c.cache == nil {
		return
	}
	data, err := json.Marshal(m)
	if err == nil {
		err = c.cache.PutKV(ctx, cacheKey(m.ID), string(data))
	}
	if err != nil {
		slog.Warn("anilist: caching media", "id", m.ID, "err", err)
	}
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"errors"`
}

// query runs a GraphQL request, waiting out one rate limit if AniList asks.
func (c *Client) query(ctx context.Context, q string, vars map[string]any, out any) error {
	payload := map[string]any{"query": q, "variables": vars}
	headers := map[string]string{"Accept": "application/json"}
	for attempt := 0; ; attempt++ {
		var resp gqlResponse
		err := c.http.PostJSON(ctx, c.url, headers, payload, &resp)

		var se *httpx.StatusError
		if errors.As(err, &se) {
			if se.StatusCode == 429 && attempt == 0 {
				if err := sleep(ctx, retryAfter(se)); err != nil {
					return err
				}
				continue
			}
			// AniList reports GraphQL errors (e.g. not found) with HTTP status codes.
			if json.Unmarshal([]byte(se.Body), &resp) == nil && len(resp.Errors) > 0 {
				err = nil
			}
		}
		if err != nil {
			return err
		}
		if len(resp.Errors) > 0 {
			if resp.Errors[0].Status == 404 {
				return ErrNotFound
			}
			return fmt.Errorf("anilist: %s", resp.Errors[0].Message)
		}
		return json.Unmarshal(resp.Data, out)
	}
}

func retryAfter(se *httpx.StatusError) time.Duration {
	if se.Header != nil {
		if n, err := strconv.Atoi(se.Header.Get("Retry-After")); err == nil && n > 0 {
			return min(time.Duration(n)*time.Second, time.Minute)
		}
	}
	return 5 * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
