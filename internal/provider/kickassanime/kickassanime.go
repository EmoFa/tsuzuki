// Package kickassanime implements the KickAssAnime provider. Its JSON API
// gives shows, episodes and a player URL; the player page holds the HLS
// manifest and subtitle tracks. See docs/providers/kickassanime.md.
package kickassanime

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/hls"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider"
)

const (
	Name = "kickassanime"
	// DefaultBaseURL is the site's current domain; it moves every so often.
	DefaultBaseURL = "https://kaa.lt"
	// playerOrigin serves the player page, the manifests and the subtitles, and
	// its CDN checks Origin and Referer on every request.
	playerOrigin = "https://krussdomi.com"
	// maxEpisodePages bounds paging for very long shows (100 episodes a page).
	maxEpisodePages = 12
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

// locale is the audio language KickAssAnime labels a mode with.
func locale(mode domain.Mode) string {
	if mode == domain.Dub {
		return "en-US"
	}
	return "ja-JP"
}

// audioLang is the ISO code of that locale, for mpv's track selection.
func audioLang(mode domain.Mode) string {
	if mode == domain.Dub {
		return "eng"
	}
	return "jpn"
}

func (p *Provider) headers() map[string]string {
	return map[string]string{"Referer": p.base + "/", "Origin": p.base}
}

type searchResult struct {
	Result []showSummary `json:"result"`
}

type showSummary struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	TitleEn       string   `json:"title_en"`
	TitleOriginal string   `json:"title_original"`
	Locales       []string `json:"locales"`
	Year          int      `json:"year"`
	Type          string   `json:"type"`
	EpisodeCount  int      `json:"episode_count"`
}

// Search finds shows available in mode.
func (p *Provider) Search(ctx context.Context, query string, mode domain.Mode) ([]domain.Show, error) {
	var resp searchResult
	body := map[string]string{"query": query}
	if err := p.client.PostJSON(ctx, p.base+"/api/fsearch", p.headers(), body, &resp); err != nil {
		return nil, fmt.Errorf("kickassanime search: %w", err)
	}
	var shows []domain.Show
	for _, s := range resp.Result {
		if !slices.Contains(s.Locales, locale(mode)) {
			continue // not available in this language
		}
		shows = append(shows, s.show())
	}
	return shows, nil
}

func (s showSummary) show() domain.Show {
	title, alts := s.Title, []string{s.TitleEn, s.TitleOriginal}
	if s.TitleEn != "" {
		title, alts = s.TitleEn, []string{s.Title, s.TitleOriginal}
	}
	show := domain.Show{
		Provider: Name,
		ID:       s.Slug,
		Title:    title,
		Type:     showType(s.Type),
		Year:     s.Year,
		Episodes: s.EpisodeCount,
	}
	for _, a := range alts {
		if a != "" && a != title {
			show.AltTitles = append(show.AltTitles, a)
		}
	}
	return show
}

// showType maps KickAssAnime's types to the labels providers share.
func showType(t string) string {
	switch strings.ToLower(t) {
	case "tv":
		return "TV"
	case "movie":
		return "Movie"
	case "ova":
		return "OVA"
	case "ona":
		return "ONA"
	case "special":
		return "Special"
	}
	return t
}

type episodeList struct {
	Pages []struct {
		Number int       `json:"number"`
		Eps    []float64 `json:"eps"`
	} `json:"pages"`
	Result []episode `json:"result"`
}

type episode struct {
	Slug          string  `json:"slug"`
	Title         string  `json:"title"`
	Number        float64 `json:"episode_number"`
	EpisodeString string  `json:"episode_string"`
	DurationMs    int64   `json:"duration_ms"`
}

// Episodes lists a show's episodes in mode, following the API's paging.
func (p *Provider) Episodes(ctx context.Context, showID string, mode domain.Mode) ([]domain.Episode, error) {
	first, err := p.episodePage(ctx, showID, mode, 1)
	if err != nil {
		return nil, err
	}
	out := episodes(first.Result)
	for i, page := range first.Pages {
		if i == 0 || i >= maxEpisodePages || len(page.Eps) == 0 {
			continue // the first page is already loaded
		}
		next, err := p.episodePage(ctx, showID, mode, page.Eps[0])
		if err != nil {
			return nil, err
		}
		out = append(out, episodes(next.Result)...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("kickassanime %s in %s: %w", showID, mode, provider.ErrNotFound)
	}
	return out, nil
}

func (p *Provider) episodePage(ctx context.Context, showID string, mode domain.Mode, ep float64) (episodeList, error) {
	u := fmt.Sprintf("%s/api/show/%s/episodes?ep=%s&lang=%s",
		p.base, url.PathEscape(showID), domain.Episode{Number: ep}.Label(), locale(mode))
	var list episodeList
	if err := p.client.GetJSON(ctx, u, p.headers(), &list); err != nil {
		return list, fmt.Errorf("kickassanime episodes: %w", err)
	}
	return list, nil
}

func episodes(eps []episode) []domain.Episode {
	out := make([]domain.Episode, 0, len(eps))
	for _, e := range eps {
		number := e.EpisodeString
		if number == "" {
			number = domain.Episode{Number: e.Number}.Label()
		}
		out = append(out, domain.Episode{
			// The episode endpoint takes this form of the ID.
			ID:       "ep-" + number + "-" + e.Slug,
			Number:   e.Number,
			Title:    e.Title,
			Duration: time.Duration(e.DurationMs) * time.Millisecond,
		})
	}
	return out
}

type episodeDetail struct {
	Servers []struct {
		Name string `json:"name"`
		Src  string `json:"src"`
	} `json:"servers"`
}

var (
	// The player page embeds its state as escaped JSON.
	manifestRE = regexp.MustCompile(`https://[\w.-]+/manifest/[\w-]+/master\.m3u8`)
	subtitleRE = regexp.MustCompile(`"language":\[0,"([\w-]+)"\],"name":\[0,"([^"]*)"\],"src":\[0,"(https://[^"]+)"\]`)
)

// Streams resolves an episode's HLS manifest and subtitle tracks.
func (p *Provider) Streams(ctx context.Context, showID string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error) {
	u := fmt.Sprintf("%s/api/show/%s/episode/%s", p.base, url.PathEscape(showID), url.PathEscape(ep.ID))
	var detail episodeDetail
	if err := p.client.GetJSON(ctx, u, p.headers(), &detail); err != nil {
		return nil, fmt.Errorf("kickassanime episode %s: %w", ep.Label(), err)
	}
	player := ""
	for _, s := range detail.Servers {
		// Other servers serve DASH, which mpv handles poorly.
		if strings.Contains(s.Src, "source=vidstream") {
			player = s.Src
			break
		}
	}
	if player == "" {
		return nil, fmt.Errorf("kickassanime episode %s: %w", ep.Label(), provider.ErrStreamsUnavailable)
	}

	page, err := p.client.Get(ctx, player, p.headers())
	if err != nil {
		return nil, fmt.Errorf("kickassanime player: %w", err)
	}
	body := html.UnescapeString(string(page))
	manifest := manifestRE.FindString(body)
	if manifest == "" {
		return nil, fmt.Errorf("kickassanime player: no manifest in the page: %w", provider.ErrNoStreams)
	}

	headers := map[string]string{"Referer": playerOrigin + "/", "Origin": playerOrigin}
	base := domain.Stream{
		URL:       manifest,
		Kind:      domain.HLS,
		Audio:     mode,
		AudioLang: audioLang(mode),
		Headers:   headers,
		Subtitles: subtitles(body),
		// The manifest holds every language's audio, so the proxy picks the
		// quality and adds the headers its CDN insists on.
		NeedsProxy: true,
	}

	master, err := p.client.Get(ctx, manifest, headers)
	if err != nil {
		return nil, fmt.Errorf("kickassanime manifest: %w", err)
	}
	heights := hls.VariantHeights(master)
	if len(heights) == 0 {
		return []domain.Stream{base}, nil
	}
	streams := make([]domain.Stream, 0, len(heights))
	for _, h := range heights {
		s := base
		s.Height, s.VariantHeight = h, h
		s.Label = fmt.Sprintf("%dp", h)
		streams = append(streams, s)
	}
	return streams, nil
}

// subtitles reads the player's subtitle tracks, ignoring its thumbnail track.
func subtitles(body string) []domain.Subtitle {
	var subs []domain.Subtitle
	seen := map[string]bool{}
	for _, m := range subtitleRE.FindAllStringSubmatch(body, -1) {
		lang, name, src := m[1], m[2], m[3]
		if seen[src] {
			continue
		}
		seen[src] = true
		subs = append(subs, domain.Subtitle{URL: src, Lang: lang, Label: name})
	}
	return subs
}
