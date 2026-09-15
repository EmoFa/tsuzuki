// Package anikoto implements the Anikoto provider. See docs/providers/anikoto.md.
package anikoto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/extractor"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider"
)

const (
	Name           = "anikoto"
	DefaultBaseURL = "https://anikototv.to"
)

// serverPreference orders the embed servers to try. All currently serve
// megaplay embeds from different CDNs.
var serverPreference = []string{"HD-1", "HD-2", "Vidstream-2"}

var (
	resultTip    = regexp.MustCompile(`data-tip="(\d+)"`)
	resultName   = regexp.MustCompile(`<a class="name d-title"[^>]*data-jp="([^"]*)"[^>]*>([^<]*)</a>`)
	resultCount  = regexp.MustCompile(`ep-status (sub|dub)"><span>\s*(\d+)`)
	resultType   = regexp.MustCompile(`<div class="right">([^<]*)</div>`)
	episodeLink  = regexp.MustCompile(`(?s)<a href="#"([^>]*data-ids="[^"]*"[^>]*)>(.*?)</a>`)
	htmlAttr     = regexp.MustCompile(`([\w-]+)="([^"]*)"`)
	episodeTitle = regexp.MustCompile(`<span class="d-title"[^>]*>([^<]*)</span>`)
	serverGroup  = regexp.MustCompile(`(?s)data-type="(\w+)".*?<ul>(.*?)</ul>`)
	serverItem   = regexp.MustCompile(`<li([^>]*)>([^<]+)</li>`)
)

type Provider struct {
	client  *httpx.Client
	sniffer extractor.Sniffer
	base    string
}

func New(client *httpx.Client, sniffer extractor.Sniffer, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Provider{client: client, sniffer: sniffer, base: strings.TrimRight(baseURL, "/")}
}

func (p *Provider) Name() string { return Name }

func (p *Provider) ajaxHeaders() map[string]string {
	return map[string]string{"Referer": p.base + "/", "X-Requested-With": "XMLHttpRequest"}
}

// ajax fetches an endpoint answering {"status":200,"result":...}.
func (p *Provider) ajax(ctx context.Context, path string, result any) error {
	var resp struct {
		Status  int             `json:"status"`
		Result  json.RawMessage `json:"result"`
		Message string          `json:"message"`
	}
	if err := p.client.GetJSON(ctx, p.base+path, p.ajaxHeaders(), &resp); err != nil {
		return err
	}
	if resp.Status != 200 {
		return fmt.Errorf("anikoto %s: status %d %s", path, resp.Status, resp.Message)
	}
	return json.Unmarshal(resp.Result, result)
}

// Search reads the full results page; the live-search endpoint caps at 5.
func (p *Provider) Search(ctx context.Context, query string, mode domain.Mode) ([]domain.Show, error) {
	page, err := p.client.Get(ctx, p.base+"/filter?keyword="+url.QueryEscape(query), map[string]string{"Referer": p.base + "/"})
	if err != nil {
		return nil, fmt.Errorf("anikoto search: %w", err)
	}
	return parseResults(string(page), mode), nil
}

func parseResults(page string, mode domain.Mode) []domain.Show {
	// Only the main list; the sidebar repeats unrelated shows.
	if i := strings.Index(page, `id="list-items"`); i >= 0 {
		page = page[i:]
	}
	var shows []domain.Show
	// Each result starts with <div class="item ">; its fields precede the next one.
	for _, block := range strings.Split(page, `<div class="item `)[1:] {
		tip := resultTip.FindStringSubmatch(block)
		name := resultName.FindStringSubmatch(block)
		if tip == nil || name == nil {
			continue
		}
		counts := map[string]int{}
		for _, c := range resultCount.FindAllStringSubmatch(block, -1) {
			counts[c[1]], _ = strconv.Atoi(c[2])
		}
		if counts[string(mode)] == 0 {
			continue
		}
		show := domain.Show{
			Provider: Name,
			ID:       tip[1],
			Title:    html.UnescapeString(strings.TrimSpace(name[2])),
			Episodes: counts[string(mode)],
		}
		if jp := html.UnescapeString(strings.TrimSpace(name[1])); jp != "" && jp != show.Title {
			show.AltTitles = []string{jp}
		}
		if t := resultType.FindStringSubmatch(block); t != nil {
			show.Type = strings.TrimSpace(t[1])
		}
		shows = append(shows, show)
	}
	return shows
}

type episodeEntry struct {
	episode domain.Episode
	malID   int
	sub     bool
	dub     bool
}

func (p *Provider) episodeEntries(ctx context.Context, showID string) ([]episodeEntry, error) {
	var listHTML string
	if err := p.ajax(ctx, "/ajax/episode/list/"+url.PathEscape(showID)+"?vrf=", &listHTML); err != nil {
		return nil, fmt.Errorf("anikoto episodes: %w", err)
	}
	var out []episodeEntry
	for _, m := range episodeLink.FindAllStringSubmatch(listHTML, -1) {
		attrs := map[string]string{}
		for _, a := range htmlAttr.FindAllStringSubmatch(m[1], -1) {
			attrs[a[1]] = a[2]
		}
		num, err := strconv.ParseFloat(attrs["data-num"], 64)
		if err != nil || attrs["data-ids"] == "" {
			continue
		}
		e := episodeEntry{
			episode: domain.Episode{
				ID:           attrs["data-ids"],
				Number:       num,
				SourceNumber: attrs["data-num"],
				Filler:       slices.Contains(strings.Fields(attrs["class"]), "filler"),
			},
			sub: attrs["data-sub"] == "1",
			dub: attrs["data-dub"] == "1",
		}
		e.malID, _ = strconv.Atoi(attrs["data-mal"])
		if t := episodeTitle.FindStringSubmatch(m[2]); t != nil {
			if title := html.UnescapeString(strings.TrimSpace(t[1])); title != "Episode "+attrs["data-num"] {
				e.episode.Title = title
			}
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("anikoto show %s: %w", showID, provider.ErrNotFound)
	}
	return out, nil
}

func (p *Provider) Episodes(ctx context.Context, showID string, mode domain.Mode) ([]domain.Episode, error) {
	entries, err := p.episodeEntries(ctx, showID)
	if err != nil {
		return nil, err
	}
	var eps []domain.Episode
	for _, e := range entries {
		if (mode == domain.Dub && e.dub) || (mode == domain.Sub && e.sub) {
			eps = append(eps, e.episode)
		}
	}
	slices.SortStableFunc(eps, func(a, b domain.Episode) int {
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

// ExternalIDs reads the MAL ID Anikoto attaches to every episode.
func (p *Provider) ExternalIDs(ctx context.Context, showID string) (anilistID, malID int, err error) {
	entries, err := p.episodeEntries(ctx, showID)
	if err != nil {
		return 0, 0, err
	}
	return 0, entries[0].malID, nil
}

type server struct {
	name   string
	linkID string
}

func (p *Provider) Streams(ctx context.Context, showID string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error) {
	var listHTML string
	if err := p.ajax(ctx, "/ajax/server/list?servers="+url.QueryEscape(ep.ID), &listHTML); err != nil {
		return nil, fmt.Errorf("anikoto servers: %w", err)
	}
	servers := parseServers(listHTML, mode)
	if len(servers) == 0 {
		return nil, fmt.Errorf("anikoto episode %s (%s): %w", ep.Label(), mode, provider.ErrNoStreams)
	}

	var errs []error
	for _, srv := range servers {
		streams, err := p.resolveServer(ctx, srv, mode)
		if err == nil {
			return streams, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, browser.ErrNoBrowser) {
			return nil, fmt.Errorf("anikoto: %w", err) // no other server can work either
		}
		slog.Info("anikoto server failed", "server", srv.name, "err", err)
		errs = append(errs, fmt.Errorf("%s: %w", srv.name, err))
	}
	return nil, fmt.Errorf("anikoto: every server failed: %w", errors.Join(errs...))
}

// parseServers returns mode's servers, preferred ones first.
func parseServers(listHTML string, mode domain.Mode) []server {
	var out []server
	for _, g := range serverGroup.FindAllStringSubmatch(listHTML, -1) {
		if g[1] != string(mode) {
			continue
		}
		for _, li := range serverItem.FindAllStringSubmatch(g[2], -1) {
			for _, a := range htmlAttr.FindAllStringSubmatch(li[1], -1) {
				if a[1] == "data-link-id" && a[2] != "" {
					out = append(out, server{name: strings.TrimSpace(li[2]), linkID: a[2]})
				}
			}
		}
	}
	rank := func(name string) int {
		if i := slices.Index(serverPreference, name); i >= 0 {
			return i
		}
		return len(serverPreference)
	}
	slices.SortStableFunc(out, func(a, b server) int { return rank(a.name) - rank(b.name) })
	return out
}

func (p *Provider) resolveServer(ctx context.Context, srv server, mode domain.Mode) ([]domain.Stream, error) {
	var res struct {
		URL string `json:"url"`
	}
	if err := p.ajax(ctx, "/ajax/server?get="+url.QueryEscape(srv.linkID), &res); err != nil {
		return nil, err
	}
	u, err := url.Parse(res.URL)
	if err != nil || !strings.Contains(u.Host, "megaplay") {
		return nil, fmt.Errorf("unsupported embed %q", res.URL)
	}
	if p.sniffer == nil {
		return nil, errors.New("megaplay embeds need a browser, and none is configured")
	}

	mp, err := extractor.ResolveMegaplay(ctx, p.sniffer, p.client, res.URL, p.base+"/")
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Referer": extractor.MegaplayOrigin}
	var subs []domain.Subtitle
	for _, t := range mp.Tracks {
		subs = append(subs, domain.Subtitle{URL: t.File, Lang: subtitleLang(t.Label), Label: t.Label})
	}
	sortSubtitles(subs)
	streams := make([]domain.Stream, 0, len(mp.Variants))
	for _, v := range mp.Variants {
		label := srv.name
		if v.Height > 0 {
			label = fmt.Sprintf("%s · %dp", srv.name, v.Height)
		}
		streams = append(streams, domain.Stream{
			URL:       v.URI,
			Kind:      domain.HLS,
			Height:    v.Height,
			Audio:     mode,
			Label:     label,
			Headers:   headers,
			Subtitles: subs,
			// Segments are MPEG-TS hidden behind fake PNG headers on image CDNs.
			NeedsProxy:      true,
			WrappedSegments: true,
		})
	}
	return streams, nil
}

// sortSubtitles orders tracks for players, which select the first: English,
// then other human translations, then machine ("(AI)") translations.
func sortSubtitles(subs []domain.Subtitle) {
	rank := func(s domain.Subtitle) int {
		switch {
		case strings.Contains(s.Label, "(AI)"):
			return 2
		case s.Lang == "en":
			return 0
		}
		return 1
	}
	slices.SortStableFunc(subs, func(a, b domain.Subtitle) int { return rank(a) - rank(b) })
}

// subtitleLang maps megaplay's track labels to language codes.
func subtitleLang(label string) string {
	switch l := strings.ToLower(label); {
	case strings.HasPrefix(l, "english"):
		return "en"
	case strings.HasPrefix(l, "spanish"):
		return "es"
	case strings.HasPrefix(l, "portuguese"):
		return "pt"
	case strings.HasPrefix(l, "french"):
		return "fr"
	case strings.HasPrefix(l, "german"):
		return "de"
	case strings.HasPrefix(l, "arabic"):
		return "ar"
	case strings.HasPrefix(l, "italian"):
		return "it"
	case strings.HasPrefix(l, "russian"):
		return "ru"
	}
	return ""
}
