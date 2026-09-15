// Package allanime implements the AllAnime provider. Stream lookup is currently
// blocked upstream (AA_CRYPTO_MISSING); see docs/providers/allanime.md.
package allanime

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider"
)

const (
	Name           = "allanime"
	DefaultAPIURL  = "https://api.allanime.day/api"
	defaultReferer = "https://allmanga.to"
)

const searchQuery = `query($search: SearchInput $limit: Int $page: Int $translationType: VaildTranslationTypeEnumType $countryOrigin: VaildCountryOriginEnumType) {
  shows(search: $search limit: $limit page: $page translationType: $translationType countryOrigin: $countryOrigin) {
    edges { _id name englishName nativeName aniListId malId type thumbnail airedStart availableEpisodes }
  }
}`

const episodesQuery = `query ($showId: String!) { show(_id: $showId) { _id availableEpisodesDetail } }`

type Provider struct {
	client *httpx.Client
	api    string
}

func New(client *httpx.Client, apiURL string) *Provider {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	return &Provider{client: client, api: apiURL}
}

func (p *Provider) Name() string { return Name }

type gqlError struct {
	Message string `json:"message"`
}

// query POSTs a GraphQL query. GET is Cloudflare-challenged; POST is not.
func (p *Provider) query(ctx context.Context, query string, vars map[string]any, data any) error {
	var resp struct {
		Data   any        `json:"data"`
		Errors []gqlError `json:"errors"`
	}
	resp.Data = data
	payload := map[string]any{"query": query, "variables": vars}
	headers := map[string]string{"Referer": defaultReferer, "Origin": defaultReferer}
	if err := p.client.PostJSON(ctx, p.api, headers, payload, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("allanime API error: %s", resp.Errors[0].Message)
	}
	return nil
}

type searchData struct {
	Shows struct {
		Edges []struct {
			ID          string  `json:"_id"`
			Name        string  `json:"name"`
			EnglishName *string `json:"englishName"`
			NativeName  *string `json:"nativeName"`
			AniListID   *string `json:"aniListId"`
			MalID       *string `json:"malId"`
			Type        *string `json:"type"`
			Thumbnail   string  `json:"thumbnail"`
			AiredStart  struct {
				Year int `json:"year"`
			} `json:"airedStart"`
			AvailableEpisodes map[string]int `json:"availableEpisodes"`
		} `json:"edges"`
	} `json:"shows"`
}

func (p *Provider) Search(ctx context.Context, query string, mode domain.Mode) ([]domain.Show, error) {
	var data searchData
	vars := map[string]any{
		"search":          map[string]any{"allowAdult": false, "allowUnknown": false, "query": query},
		"limit":           40,
		"page":            1,
		"translationType": string(mode),
		"countryOrigin":   "ALL",
	}
	if err := p.query(ctx, searchQuery, vars, &data); err != nil {
		return nil, fmt.Errorf("allanime search: %w", err)
	}
	shows := make([]domain.Show, 0, len(data.Shows.Edges))
	for _, e := range data.Shows.Edges {
		show := domain.Show{
			Provider:  Name,
			ID:        e.ID,
			Title:     e.Name,
			Type:      deref(e.Type),
			Year:      e.AiredStart.Year,
			Episodes:  e.AvailableEpisodes[string(mode)],
			Poster:    e.Thumbnail,
			AniListID: atoi(deref(e.AniListID)),
			MalID:     atoi(deref(e.MalID)),
		}
		for _, alt := range []string{deref(e.EnglishName), deref(e.NativeName)} {
			if alt != "" && alt != show.Title {
				show.AltTitles = append(show.AltTitles, alt)
			}
		}
		shows = append(shows, show)
	}
	return shows, nil
}

type episodesData struct {
	Show *struct {
		AvailableEpisodesDetail map[string][]string `json:"availableEpisodesDetail"`
	} `json:"show"`
}

func (p *Provider) Episodes(ctx context.Context, showID string, mode domain.Mode) ([]domain.Episode, error) {
	var data episodesData
	if err := p.query(ctx, episodesQuery, map[string]any{"showId": showID}, &data); err != nil {
		return nil, fmt.Errorf("allanime episodes: %w", err)
	}
	if data.Show == nil {
		return nil, fmt.Errorf("allanime show %s: %w", showID, provider.ErrNotFound)
	}
	var eps []domain.Episode
	for _, s := range data.Show.AvailableEpisodesDetail[string(mode)] {
		n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			continue // non-numeric specials aren't addressable by number
		}
		// AllAnime addresses episodes by (show, number string, mode).
		eps = append(eps, domain.Episode{ID: s, Number: n, SourceNumber: s})
	}
	slices.SortFunc(eps, func(a, b domain.Episode) int {
		switch {
		case a.Number < b.Number:
			return -1
		case a.Number > b.Number:
			return 1
		}
		return 0
	})
	return eps, nil
}

// Streams is unavailable: the episode query now fails with AA_CRYPTO_MISSING
// and the required request-side crypto isn't known yet.
func (p *Provider) Streams(context.Context, string, domain.Episode, domain.Mode) ([]domain.Stream, error) {
	return nil, fmt.Errorf("allanime: episode sources are blocked upstream (AA_CRYPTO_MISSING): %w", provider.ErrStreamsUnavailable)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
