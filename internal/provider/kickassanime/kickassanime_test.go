package kickassanime

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider/providertest"
)

const (
	showSlug   = "sousou-no-frieren-2d15"
	episodeID  = "ep-1-f897b3"
	playerPath = "krussdomi.com/cat-player/player?id=67d0c079169c31976b8d7970&source=vidstream&ln=ja-JP"
	manifestID = "67d0c079169c31976b8d7970"
)

func fixture(t *testing.T, name, contentType string) func(*http.Request) (*http.Response, error) {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return func(r *http.Request) (*http.Response, error) {
		return providertest.Respond(r, http.StatusOK, contentType, body), nil
	}
}

func fixtureProvider(t *testing.T) *Provider {
	t.Helper()
	search := fixture(t, "fsearch_frieren.json", "application/json")
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"kaa.lt/api/fsearch": func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Errorf("search method = %s, want POST", r.Method)
			}
			if r.Header.Get("Referer") == "" {
				t.Error("search sent without a Referer")
			}
			return search(r)
		},
		"kaa.lt/api/show/" + showSlug + "/episodes?ep=1&lang=ja-JP": fixture(t, "episodes_ja.json", "application/json"),
		"kaa.lt/api/show/" + showSlug + "/episode/" + episodeID:     fixture(t, "episode_ep1.json", "application/json"),
		playerPath: fixture(t, "player.html", "text/html"),
		"hls.krussdomi.com/manifest/" + manifestID + "/master.m3u8": func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Origin") == "" {
				t.Error("manifest fetched without an Origin: its CDN returns 403")
			}
			return fixture(t, "master.m3u8", "application/x-mpegURL")(r)
		},
	}}
	return New(httpx.New(httpx.Options{Transport: routes}), "")
}

func TestContract(t *testing.T) {
	providertest.Run(t, fixtureProvider(t), providertest.Case{
		Query:       "frieren",
		Mode:        domain.Sub,
		ShowTitle:   "Frieren: Beyond Journey's End",
		MinEpisodes: 28,
		Episode:     1,
	})
}

func TestSearchFiltersByLanguage(t *testing.T) {
	p := fixtureProvider(t)
	ctx := context.Background()
	subs, err := p.Search(ctx, "frieren", domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) == 0 || subs[0].ID != showSlug || subs[0].Type != "TV" || subs[0].Year != 2023 {
		t.Fatalf("sub results = %+v", subs)
	}
	if !slices.Contains(subs[0].AltTitles, "Sousou no Frieren") {
		t.Errorf("alt titles = %v, want the romaji title for matching", subs[0].AltTitles)
	}
	dubs, err := p.Search(ctx, "frieren", domain.Dub)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has shows without an en-US locale; they can't be watched dubbed.
	if len(dubs) >= len(subs) {
		t.Errorf("dub results (%d) should be fewer than sub results (%d)", len(dubs), len(subs))
	}
}

func TestEpisodesAndStreams(t *testing.T) {
	p := fixtureProvider(t)
	ctx := context.Background()
	eps, err := p.Episodes(ctx, showSlug, domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 28 || eps[0].ID != episodeID || eps[0].Title == "" || eps[0].Duration == 0 {
		t.Fatalf("episodes[0] = %+v (of %d)", eps[0], len(eps))
	}

	streams, err := p.Streams(ctx, showSlug, eps[0], domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	var heights []int
	for _, s := range streams {
		heights = append(heights, s.Height)
		if s.Kind != domain.HLS || !s.NeedsProxy || s.Headers["Origin"] == "" || s.Headers["Referer"] == "" {
			t.Errorf("stream %+v needs the player's Origin and Referer through the proxy", s)
		}
		if s.AudioLang != "jpn" {
			t.Errorf("sub stream audio = %q, want jpn (the manifest holds every language)", s.AudioLang)
		}
		if !strings.HasSuffix(s.URL, "master.m3u8") || s.VariantHeight != s.Height {
			t.Errorf("stream URL/variant = %q/%d", s.URL, s.VariantHeight)
		}
	}
	slices.Sort(heights)
	if !slices.Equal(heights, []int{360, 720, 1080}) {
		t.Fatalf("heights = %v", heights)
	}

	subs := streams[0].Subtitles
	if len(subs) < 5 {
		t.Fatalf("subtitles = %+v", subs)
	}
	var langs []string
	for _, s := range subs {
		langs = append(langs, s.Lang)
		if !strings.HasSuffix(s.URL, ".vtt") || s.Label == "" {
			t.Errorf("subtitle %+v", s)
		}
		if strings.Contains(s.URL, "preview-") {
			t.Error("the thumbnail track was taken for a subtitle")
		}
	}
	if !slices.Contains(langs, "en") || !slices.Contains(langs, "de") {
		t.Errorf("subtitle languages = %v", langs)
	}
}

func TestDubUsesEnglishAudio(t *testing.T) {
	if got := audioLang(domain.Dub); got != "eng" {
		t.Errorf("dub audio = %q", got)
	}
	if got := locale(domain.Dub); got != "en-US" {
		t.Errorf("dub locale = %q", got)
	}
}
