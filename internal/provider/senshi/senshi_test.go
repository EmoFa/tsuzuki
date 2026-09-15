package senshi

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/hls"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
)

func fixture(t *testing.T, name string) func(*http.Request) (*http.Response, error) {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Origin") != "https://senshi.to" {
			t.Errorf("request to %s without Origin", r.URL)
		}
		return providertest.Respond(r, http.StatusOK, "application/json", body), nil
	}
}

func fixtureProvider(t *testing.T) *Provider {
	t.Helper()
	master, err := os.ReadFile("testdata/master_21656.txt")
	if err != nil {
		t.Fatal(err)
	}
	filter := fixture(t, "filter_frieren.json")
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"senshi.to/anime/filter": func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Errorf("search method = %s", r.Method)
			}
			resp, err := filter(r)
			resp.StatusCode = http.StatusCreated // Senshi answers searches with 201
			return resp, err
		},
		"senshi.to/anime/73b5b":              fixture(t, "anime_73b5b.json"),
		"senshi.to/episodes/59978":           fixture(t, "episodes_59978.json"),
		"senshi.to/episode-embeds/59978/1":   fixture(t, "embeds_59978_1.json"),
		"s.vidcloud.se/_v1/sources?id=21656": fixture(t, "sources_21656.json"),
	}}
	// The master URL carries a token; route it by prefix.
	return New(httpx.New(httpx.Options{Transport: masterRoute{routes, master}}), "", "")
}

type masterRoute struct {
	next   providertest.Routes
	master []byte
}

func (m masterRoute) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/master.txt") {
		return providertest.Respond(r, http.StatusOK, "application/octet-stream", m.master), nil
	}
	return m.next.RoundTrip(r)
}

func TestContractFixtures(t *testing.T) {
	for _, mode := range []domain.Mode{domain.Sub, domain.Dub} {
		t.Run(string(mode), func(t *testing.T) {
			providertest.Run(t, fixtureProvider(t), providertest.Case{
				Query:       "frieren",
				Mode:        mode,
				ShowTitle:   "Sousou no Frieren 2nd Season",
				MinEpisodes: 10,
				Episode:     1,
			})
		})
	}
}

func TestStreamsDetails(t *testing.T) {
	p := fixtureProvider(t)
	ctx := context.Background()

	sub, err := p.Streams(ctx, "73b5b", domain.Episode{ID: "1", Number: 1}, domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 2 || sub[0].Height != 1080 || sub[1].VariantHeight != 480 {
		t.Fatalf("sub streams = %+v", sub)
	}
	s := sub[0]
	if s.AudioLang != "ja" || !s.NeedsProxy || s.Playlist == nil || s.Headers["Origin"] != "https://senshi.to" {
		t.Errorf("stream = %+v", s)
	}
	if len(s.Subtitles) != 1 || !strings.HasSuffix(s.Subtitles[0].URL, "sub_en.ass") {
		t.Errorf("sub subtitles = %+v", s.Subtitles)
	}

	dub, err := p.Streams(ctx, "73b5b", domain.Episode{ID: "1", Number: 1}, domain.Dub)
	if err != nil {
		t.Fatal(err)
	}
	if dub[0].AudioLang != "en" || len(dub[0].Subtitles) != 1 || !strings.HasSuffix(dub[0].Subtitles[0].URL, "ai_dub.ass") {
		t.Errorf("dub stream = %+v", dub[0])
	}
}

func TestDecodePlaylistFixture(t *testing.T) {
	enc, err := os.ReadFile("testdata/master_21656.txt")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decodePlaylist(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !hls.IsPlaylist(plain) || !strings.Contains(string(plain), `LANGUAGE="en"`) {
		t.Fatalf("decoded:\n%s", plain)
	}
	if got, _ := decodePlaylist([]byte("#EXTM3U\n")); string(got) != "#EXTM3U\n" {
		t.Error("plain playlist should pass through")
	}
	if _, err := decodePlaylist([]byte("EM3U8v1:AAAA")); err == nil {
		t.Error("truncated payload should fail")
	}
}

func TestEpisodesDetails(t *testing.T) {
	eps, err := fixtureProvider(t).Episodes(context.Background(), "73b5b", domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 10 || eps[0].Title != "Shall We Go, Then?" || eps[0].ID != "1" {
		t.Fatalf("eps = %+v", eps[:1])
	}
}

func TestExternalIDs(t *testing.T) {
	al, mal, err := fixtureProvider(t).ExternalIDs(context.Background(), "73b5b")
	if err != nil || al != 182255 || mal != 59978 {
		t.Fatalf("anilist=%d mal=%d err=%v", al, mal, err)
	}
}

func TestSubtitleLang(t *testing.T) {
	for u, want := range map[string]string{
		"https://s.anicdn.se/uploads/attachments/x/sub_en.ass": "en",
		"https://s.anicdn.se/uploads/attachments/x/sub_de.ass": "de",
		"https://s.anicdn.se/uploads/attachments/x/ai_dub.ass": "en",
	} {
		if got := subtitleLang(u); got != want {
			t.Errorf("subtitleLang(%s) = %q, want %q", u, got, want)
		}
	}
}
