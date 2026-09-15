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

var (
	ErrNotFound = errors.New("anilist: not found")
	// ErrUnauthorized means the access token is missing, expired or revoked.
	ErrUnauthorized = errors.New("anilist: not logged in or token expired")
)

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
	cache Cache  // optional
	token string // for authenticated requests
}

// WithURL returns a client for a different GraphQL endpoint (for testing).
func (c *Client) WithURL(url string) *Client {
	cp := *c
	cp.url = url
	return &cp
}

// WithToken returns a client that sends token with every request.
func (c *Client) WithToken(token string) *Client {
	cp := *c
	cp.token = token
	return &cp
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
	if c.token != "" {
		headers["Authorization"] = "Bearer " + c.token
	}
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
			if se.StatusCode == 401 {
				return ErrUnauthorized
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
			switch msg := strings.ToLower(resp.Errors[0].Message); {
			case resp.Errors[0].Status == 404:
				return ErrNotFound
			case resp.Errors[0].Status == 401, strings.Contains(msg, "invalid token"), strings.Contains(msg, "unauthorized"):
				return ErrUnauthorized
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

// User is the logged-in AniList user.
type User struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	SiteURL string `json:"siteUrl"`
}

// Viewer returns the user the token belongs to.
func (c *Client) Viewer(ctx context.Context) (User, error) {
	var data struct {
		Viewer *User `json:"Viewer"`
	}
	if err := c.query(ctx, `query { Viewer { id name siteUrl } }`, nil, &data); err != nil {
		return User{}, fmt.Errorf("anilist viewer: %w", err)
	}
	if data.Viewer == nil {
		return User{}, ErrUnauthorized
	}
	return *data.Viewer, nil
}

// SaveListEntry sets a show's status and progress on the logged-in user's list.
func (c *Client) SaveListEntry(ctx context.Context, mediaID int, status string, progress int) error {
	var data struct {
		Entry *struct {
			ID int `json:"id"`
		} `json:"SaveMediaListEntry"`
	}
	q := `mutation ($mediaId: Int, $status: MediaListStatus, $progress: Int) {
		SaveMediaListEntry(mediaId: $mediaId, status: $status, progress: $progress) { id status progress } }`
	vars := map[string]any{"mediaId": mediaID, "status": status, "progress": progress}
	if err := c.query(ctx, q, vars, &data); err != nil {
		return fmt.Errorf("anilist save list entry: %w", err)
	}
	if data.Entry == nil {
		return errors.New("anilist save list entry: no entry returned")
	}
	return nil
}

// ListItem is an entry on a user's AniList anime list.
type ListItem struct {
	MediaID   int
	Status    string
	Progress  int
	Score     float64 // out of 10
	UpdatedAt time.Time
	Media     Media
}

// UserList fetches every entry on a user's anime list. Media details are cached.
func (c *Client) UserList(ctx context.Context, userID int) ([]ListItem, error) {
	var data struct {
		Collection struct {
			Lists []struct {
				Entries []struct {
					MediaID   int     `json:"mediaId"`
					Status    string  `json:"status"`
					Progress  int     `json:"progress"`
					Score     float64 `json:"score"`
					UpdatedAt int64   `json:"updatedAt"`
					Media     Media   `json:"media"`
				} `json:"entries"`
			} `json:"lists"`
		} `json:"MediaListCollection"`
	}
	q := `query ($userId: Int) { MediaListCollection(userId: $userId, type: ANIME) { lists { entries {
		mediaId status progress score(format: POINT_10_DECIMAL) updatedAt media { ` + mediaFields + ` } } } } }`
	if err := c.query(ctx, q, map[string]any{"userId": userID}, &data); err != nil {
		return nil, fmt.Errorf("anilist user list: %w", err)
	}
	seen := map[int]bool{}
	var out []ListItem
	for _, l := range data.Collection.Lists {
		// A show can appear in custom lists too; keep the first.
		for _, e := range l.Entries {
			if seen[e.MediaID] {
				continue
			}
			seen[e.MediaID] = true
			// Only cache complete details; never replace good cached ones with a stub.
			if e.Media.ID != 0 && e.Media.DisplayTitle() != "" {
				c.store(ctx, e.Media)
			}
			item := ListItem{MediaID: e.MediaID, Status: e.Status, Progress: e.Progress, Score: e.Score, Media: e.Media}
			if e.UpdatedAt > 0 {
				item.UpdatedAt = time.Unix(e.UpdatedAt, 0)
			}
			out = append(out, item)
		}
	}
	return out, nil
}
