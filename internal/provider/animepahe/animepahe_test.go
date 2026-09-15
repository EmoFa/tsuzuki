package animepahe

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
)

const (
	frierenShow = "92846367-9f7f-4760-8477-e7af86f1a2f8"
	frierenEp1  = "a45b0845c851d42da7c573fc4d2516efcdcb3d5e6dfe5bc09620b89ea9a6ff1e"
)

func fixture(t *testing.T, contentType, name string) func(*http.Request) (*http.Response, error) {
	t.Helper()
	path := "testdata/" + name
	if strings.HasPrefix(name, "../") {
		path = name
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return func(r *http.Request) (*http.Response, error) {
		if r.Referer() == "" {
			t.Errorf("request to %s without Referer", r.URL)
		}
		return providertest.Respond(r, http.StatusOK, contentType, body), nil
	}
}

func fixtureProvider(t *testing.T) *Provider {
	t.Helper()
	const js, htm = "application/json", "text/html"
	kwik := fixture(t, htm, "../../extractor/testdata/kwik_embed.html")
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"animepahe.pw/api?m=search&q=frieren":                                       fixture(t, js, "search_frieren.json"),
		"animepahe.pw/api?m=release&id=" + frierenShow + "&sort=episode_asc&page=1": fixture(t, js, "release_frieren.json"),
		"animepahe.pw/play/" + frierenShow + "/" + frierenEp1:                       fixture(t, htm, "play_frieren_1.html"),
		"animepahe.pw/anime/" + frierenShow:                                         fixture(t, htm, "anime_frieren.html"),
		"kwik.cx/e/aeNSh4eblrse":                                                    kwik,
		"kwik.cx/e/d3ccaeXzK7o4":                                                    kwik,
		"kwik.cx/e/iKAcIjd2ce0f":                                                    kwik,
		"kwik.cx/e/3GexHqPVlLPw":                                                    kwik,
		"kwik.cx/e/WO0e7DkW9dRm":                                                    kwik,
		"kwik.cx/e/TmZ9ymbik413":                                                    kwik,
	}}
	return New(httpx.New(httpx.Options{Transport: routes}), "")
}

func TestContractFixtures(t *testing.T) {
	for _, mode := range []domain.Mode{domain.Sub, domain.Dub} {
		t.Run(string(mode), func(t *testing.T) {
			providertest.Run(t, fixtureProvider(t), providertest.Case{
				Query:       "frieren",
				Mode:        mode,
				ShowTitle:   "Frieren: Beyond Journey's End",
				MinEpisodes: 28,
				Episode:     1,
			})
		})
	}
}

func TestStreamsDetails(t *testing.T) {
	p := fixtureProvider(t)
	streams, err := p.Streams(context.Background(), frierenShow, domain.Episode{ID: frierenEp1, Number: 1}, domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 3 {
		t.Fatalf("got %d streams, want 3 (360/720/1080 jpn)", len(streams))
	}
	best, err := domain.SelectStream(streams, "best")
	if err != nil {
		t.Fatal(err)
	}
	if best.Height != 1080 || best.Label != "SEV · 1080p" {
		t.Errorf("best = %+v", best)
	}
	if best.Headers["Referer"] != "https://kwik.cx/" || !best.NeedsProxy {
		t.Errorf("stream must carry kwik Referer and need the proxy: %+v", best)
	}
}

func TestEpisodesDetails(t *testing.T) {
	eps, err := fixtureProvider(t).Episodes(context.Background(), frierenShow, domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	first := eps[0]
	if first.Number != 1 || first.ID != frierenEp1 || first.Duration != 26*time.Minute+time.Second {
		t.Errorf("first = %+v", first)
	}
}

func TestExternalIDs(t *testing.T) {
	al, mal, err := fixtureProvider(t).ExternalIDs(context.Background(), frierenShow)
	if err != nil {
		t.Fatal(err)
	}
	if al != 154587 || mal != 52991 {
		t.Fatalf("anilist=%d mal=%d", al, mal)
	}
}

func TestSeasonTwoIsRenumbered(t *testing.T) {
	body, err := os.ReadFile("testdata/release_frieren_s2.json")
	if err != nil {
		t.Fatal(err)
	}
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"animepahe.pw/api?m=release&id=s2&sort=episode_asc&page=1": func(r *http.Request) (*http.Response, error) {
			return providertest.Respond(r, 200, "application/json", body), nil
		},
	}}
	p := New(httpx.New(httpx.Options{Transport: routes}), "")
	eps, err := p.Episodes(context.Background(), "s2", domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 10 || eps[0].Number != 1 || eps[0].SourceNumber != "29" || eps[9].Number != 10 {
		t.Fatalf("first=%+v last=%+v", eps[0], eps[len(eps)-1])
	}
}

func TestEpisodesPaginate(t *testing.T) {
	page := func(last int, nums ...int) []byte {
		var items []string
		for _, n := range nums {
			items = append(items, `{"episode":`+strconv.Itoa(n)+`,"session":"s`+strconv.Itoa(n)+`","duration":"00:24:00","filler":0}`)
		}
		return []byte(`{"last_page":` + strconv.Itoa(last) + `,"data":[` + strings.Join(items, ",") + `]}`)
	}
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"animepahe.pw/api?m=release&id=x&sort=episode_asc&page=1": func(r *http.Request) (*http.Response, error) {
			return providertest.Respond(r, 200, "application/json", page(2, 1, 2)), nil
		},
		"animepahe.pw/api?m=release&id=x&sort=episode_asc&page=2": func(r *http.Request) (*http.Response, error) {
			return providertest.Respond(r, 200, "application/json", page(2, 3)), nil
		},
	}}
	eps, err := New(httpx.New(httpx.Options{Transport: routes}), "").Episodes(context.Background(), "x", domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 3 || eps[2].ID != "s3" {
		t.Fatalf("eps = %+v", eps)
	}
}
