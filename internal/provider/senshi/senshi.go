// Package senshi implements the Senshi provider. See docs/providers/senshi.md.
package senshi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/hls"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider"
)

const (
	Name              = "senshi"
	DefaultBaseURL    = "https://senshi.to"
	DefaultSourcesURL = "https://s.vidcloud.se/_v1/sources"
)

// Playlist encryption used by Senshi's player: AES-256-GCM, key = keyA XOR keyB
// (both embedded in the web client), body = "EM3U8v1:" + base64(iv(12) | ct | tag).
var (
	playlistPrefix = []byte("EM3U8v1:")
	keyA           = [32]byte{226, 24, 149, 40, 170, 108, 184, 157, 168, 18, 90, 64, 186, 69, 66, 110, 109, 169, 203, 138, 29, 188, 78, 25, 203, 185, 211, 252, 76, 126, 134, 42}
	keyB           = [32]byte{140, 250, 231, 59, 141, 129, 254, 6, 30, 203, 96, 249, 13, 237, 122, 106, 60, 57, 126, 48, 152, 101, 128, 186, 122, 88, 171, 249, 187, 202, 40, 220}
)

type Provider struct {
	client  *httpx.Client
	base    string
	sources string

	mu    sync.Mutex
	shows map[string]animeDetail // by public ID
}

func New(client *httpx.Client, baseURL, sourcesURL string) *Provider {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if sourcesURL == "" {
		sourcesURL = DefaultSourcesURL
	}
	return &Provider{
		client:  client,
		base:    strings.TrimRight(baseURL, "/"),
		sources: sourcesURL,
		shows:   map[string]animeDetail{},
	}
}

func (p *Provider) Name() string { return Name }

// headers are what Senshi's own player sends. The media hosts' firewall
// rejects requests without the Origin.
func (p *Provider) headers() map[string]string {
	return map[string]string{"Referer": p.base + "/", "Origin": p.base}
}

type animeSummary struct {
	ID       int    `json:"id"` // MyAnimeList ID
	PublicID string `json:"public_id"`
	Title    string `json:"title"`
	TitleEn  string `json:"title_english"`
	Synonyms string `json:"synonyms"`
	Type     string `json:"type"`
	Year     int    `json:"ani_year"`
	Picture  string `json:"anime_picture"`
	SubCount int    `json:"sub_count"`
	DubCount int    `json:"dub_count"`
}

type animeDetail struct {
	animeSummary
	AniListID int `json:"anilist_id"`
}

func (a animeSummary) count(mode domain.Mode) int {
	if mode == domain.Dub {
		return a.DubCount
	}
	return a.SubCount
}

func (p *Provider) Search(ctx context.Context, query string, mode domain.Mode) ([]domain.Show, error) {
	var resp struct {
		Data []animeSummary `json:"data"`
	}
	body := map[string]any{"searchTerm": query, "page": 1, "limit": 40}
	if err := p.client.PostJSON(ctx, p.base+"/anime/filter", p.headers(), body, &resp); err != nil {
		return nil, fmt.Errorf("senshi search: %w", err)
	}
	var shows []domain.Show
	for _, a := range resp.Data {
		if a.count(mode) == 0 {
			continue
		}
		show := domain.Show{
			Provider: Name,
			ID:       a.PublicID,
			Title:    a.Title,
			Type:     a.Type,
			Year:     a.Year,
			Episodes: a.count(mode),
			MalID:    a.ID,
		}
		if a.Picture != "" {
			show.Poster = p.base + a.Picture
		}
		for _, alt := range append([]string{a.TitleEn}, strings.Split(a.Synonyms, ",")...) {
			if alt = strings.TrimSpace(alt); alt != "" && alt != show.Title {
				show.AltTitles = append(show.AltTitles, alt)
			}
		}
		shows = append(shows, show)
	}
	return shows, nil
}

// detail fetches (and caches) a show by public ID; the other endpoints need its
// MAL ID.
func (p *Provider) detail(ctx context.Context, publicID string) (animeDetail, error) {
	p.mu.Lock()
	d, ok := p.shows[publicID]
	p.mu.Unlock()
	if ok {
		return d, nil
	}
	err := p.client.GetJSON(ctx, p.base+"/anime/"+url.PathEscape(publicID), p.headers(), &d)
	var se *httpx.StatusError
	if errors.As(err, &se) && se.StatusCode == 404 {
		return d, fmt.Errorf("senshi show %s: %w", publicID, provider.ErrNotFound)
	}
	if err != nil {
		return d, fmt.Errorf("senshi show: %w", err)
	}
	p.mu.Lock()
	p.shows[publicID] = d
	p.mu.Unlock()
	return d, nil
}

func (p *Provider) ExternalIDs(ctx context.Context, showID string) (anilistID, malID int, err error) {
	d, err := p.detail(ctx, showID)
	return d.AniListID, d.ID, err
}

type episode struct {
	Number int    `json:"ep_id"`
	Title  string `json:"ep_title"`
	Filler bool   `json:"ep_filler"`
	Recap  bool   `json:"ep_recap"`
}

// Episodes lists episodes up to the show's sub or dub count; Senshi reports
// availability per mode only as a count.
func (p *Provider) Episodes(ctx context.Context, showID string, mode domain.Mode) ([]domain.Episode, error) {
	d, err := p.detail(ctx, showID)
	if err != nil {
		return nil, err
	}
	var raw []episode
	if err := p.client.GetJSON(ctx, fmt.Sprintf("%s/episodes/%d", p.base, d.ID), p.headers(), &raw); err != nil {
		return nil, fmt.Errorf("senshi episodes: %w", err)
	}
	slices.SortFunc(raw, func(a, b episode) int { return a.Number - b.Number })
	var eps []domain.Episode
	for _, e := range raw {
		if e.Number > d.count(mode) {
			break
		}
		n := strconv.Itoa(e.Number)
		eps = append(eps, domain.Episode{ID: n, Number: float64(e.Number), SourceNumber: n, Title: e.Title, Filler: e.Filler, Recap: e.Recap})
	}
	return eps, nil
}

type embed struct {
	SourceID int    `json:"remote_source_id"`
	Status   string `json:"status"` // "Dub", "HardSub", ...
}

type sourceResponse struct {
	Source struct {
		Src     string `json:"src"`
		Quality string `json:"quality"`
		Audio   string `json:"audio"` // "both" when the master has ja + en renditions
	} `json:"source"`
	Tracks []struct {
		URL   string `json:"url"`
		Label string `json:"label"`
	} `json:"tracks"`
}

func (p *Provider) Streams(ctx context.Context, showID string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error) {
	d, err := p.detail(ctx, showID)
	if err != nil {
		return nil, err
	}
	var embeds []embed
	if err := p.client.GetJSON(ctx, fmt.Sprintf("%s/episode-embeds/%d/%s", p.base, d.ID, ep.ID), p.headers(), &embeds); err != nil {
		return nil, fmt.Errorf("senshi embeds: %w", err)
	}
	var sourceIDs []int
	for _, e := range embeds {
		isDub := strings.EqualFold(e.Status, "dub")
		if isDub == (mode == domain.Dub) && e.SourceID != 0 && !slices.Contains(sourceIDs, e.SourceID) {
			sourceIDs = append(sourceIDs, e.SourceID)
		}
	}
	if len(sourceIDs) == 0 {
		return nil, fmt.Errorf("senshi episode %s (%s): %w", ep.Label(), mode, provider.ErrNoStreams)
	}

	var streams []domain.Stream
	var errs []error
	for _, id := range sourceIDs {
		s, err := p.resolveSource(ctx, id, mode)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		streams = append(streams, s...)
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("senshi sources: %w", errors.Join(errs...))
	}
	return streams, nil
}

// resolveSource returns one stream per video variant of a source's master.
func (p *Provider) resolveSource(ctx context.Context, sourceID int, mode domain.Mode) ([]domain.Stream, error) {
	var resp []sourceResponse
	if err := p.client.GetJSON(ctx, fmt.Sprintf("%s?id=%d", p.sources, sourceID), p.headers(), &resp); err != nil {
		return nil, err
	}
	if len(resp) == 0 || resp[0].Source.Src == "" {
		return nil, fmt.Errorf("source %d: empty response", sourceID)
	}
	src := resp[0]

	master, err := p.client.Get(ctx, src.Source.Src, p.headers())
	if err != nil {
		return nil, fmt.Errorf("source %d master: %w", sourceID, err)
	}
	if master, err = decodePlaylist(master); err != nil {
		return nil, fmt.Errorf("source %d master: %w", sourceID, err)
	}

	base := domain.Stream{
		URL:        src.Source.Src,
		Kind:       domain.HLS,
		Audio:      mode,
		Headers:    p.headers(),
		NeedsProxy: true, // playlists are encrypted
		Playlist:   &domain.PlaylistCodec{Prefix: playlistPrefix, Decode: decodePlaylist},
		Subtitles:  subtitles(src, mode),
	}
	if src.Source.Audio == "both" {
		base.AudioLang = "ja"
		if mode == domain.Dub {
			base.AudioLang = "en"
		}
	}

	heights := hls.VariantHeights(master)
	if len(heights) == 0 {
		base.Height, _ = strconv.Atoi(strings.TrimSuffix(src.Source.Quality, "p"))
		base.Label = src.Source.Quality
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

// subtitles picks the tracks Senshi's player offers for mode: dub mode shows
// only dub-labelled tracks, sub mode everything else. "chapter" is storyboard data.
func subtitles(src sourceResponse, mode domain.Mode) []domain.Subtitle {
	var subs []domain.Subtitle
	for _, t := range src.Tracks {
		if t.URL == "" || t.Label == "chapter" {
			continue
		}
		isDub := strings.Contains(strings.ToLower(t.Label), "dub") || strings.Contains(t.URL, "ai_dub")
		if isDub != (mode == domain.Dub) {
			continue
		}
		subs = append(subs, domain.Subtitle{URL: t.URL, Lang: subtitleLang(t.URL), Label: t.Label})
	}
	return subs
}

var subLangFile = regexp.MustCompile(`/sub_([a-z]{2,3})\.\w+$`)

// subtitleLang reads the language from Senshi's file names ("sub_de.ass").
// The AI dub track is English.
func subtitleLang(u string) string {
	if m := subLangFile.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return "en"
}

// decodePlaylist decrypts an "EM3U8v1:" playlist; other bodies pass through.
func decodePlaylist(body []byte) ([]byte, error) {
	trimmed := []byte(strings.TrimSpace(string(body)))
	if !strings.HasPrefix(string(trimmed), string(playlistPrefix)) {
		return body, nil
	}
	data, err := base64.StdEncoding.DecodeString(string(trimmed[len(playlistPrefix):]))
	if err != nil {
		return nil, fmt.Errorf("decoding playlist: %w", err)
	}
	var key [32]byte
	for i := range key {
		key[i] = keyA[i] ^ keyB[i]
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead()+1 {
		return nil, errors.New("encrypted playlist too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting playlist: %w", err)
	}
	return plain, nil
}
