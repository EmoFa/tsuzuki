package anikoto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
)

type fakeSniffer struct {
	calls []string
}

func (f *fakeSniffer) Sniff(_ context.Context, req browser.SniffRequest) (map[string]browser.Captured, error) {
	f.calls = append(f.calls, req.URL)
	sources, err := os.ReadFile("../../extractor/testdata/megaplay_getsources.json")
	if err != nil {
		return nil, err
	}
	return map[string]browser.Captured{
		"sources": {Body: sources},
		"master":  {URL: "https://megap.mikora.top/806c/c512/master.m3u8?token=abc"},
	}, nil
}

func file(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureProvider(t *testing.T) (*Provider, *fakeSniffer) {
	t.Helper()
	serve := func(body []byte, ctype string, ajax bool) func(*http.Request) (*http.Response, error) {
		return func(r *http.Request) (*http.Response, error) {
			if ajax && r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Errorf("%s without X-Requested-With", r.URL)
			}
			return providertest.Respond(r, http.StatusOK, ctype, body), nil
		}
	}
	servers := string(file(t, "testdata/servers_8384_1.json"))
	link := linkFor(t, servers)
	eps := file(t, "testdata/episodes_8384.json")
	ep1IDs := idsFor(t, string(eps))

	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"anikototv.to/filter?keyword=frieren":                              serve(file(t, "testdata/filter_frieren.html"), "text/html", false),
		"anikototv.to/ajax/episode/list/8384?vrf=":                         serve(eps, "application/json", true),
		"anikototv.to/ajax/server/list?servers=" + url.QueryEscape(ep1IDs): serve([]byte(servers), "application/json", true),
		"anikototv.to/ajax/server?get=" + url.QueryEscape(link):            serve(file(t, "testdata/server_hd1.json"), "application/json", true),
		"megap.mikora.top/806c/c512/master.m3u8?token=abc": func(r *http.Request) (*http.Response, error) {
			if r.Referer() != "https://megaplay.buzz/" {
				t.Errorf("master fetched without megaplay Referer")
			}
			return providertest.Respond(r, 200, "application/vnd.apple.mpegurl", file(t, "../../extractor/testdata/megaplay_master.m3u8")), nil
		},
	}}
	sn := &fakeSniffer{}
	return New(httpx.New(httpx.Options{Transport: routes}), sn, ""), sn
}

// linkFor returns the HD-1 sub server link from the fixture.
func linkFor(t *testing.T, serversJSON string) string {
	t.Helper()
	var s struct{ Result string }
	decode(t, serversJSON, &s)
	for _, srv := range parseServers(s.Result, domain.Sub) {
		if srv.name == "HD-1" {
			return srv.linkID
		}
	}
	t.Fatal("HD-1 not in fixture")
	return ""
}

func idsFor(t *testing.T, episodesJSON string) string {
	t.Helper()
	var s struct{ Result string }
	decode(t, episodesJSON, &s)
	m := episodeLink.FindStringSubmatch(s.Result)
	for _, a := range htmlAttr.FindAllStringSubmatch(m[1], -1) {
		if a[1] == "data-ids" {
			return a[2]
		}
	}
	t.Fatal("no data-ids")
	return ""
}

func TestContractFixtures(t *testing.T) {
	p, sn := fixtureProvider(t)
	providertest.Run(t, p, providertest.Case{
		Query:       "frieren",
		Mode:        domain.Sub,
		ShowTitle:   "Frieren: Beyond Journey's End Season 2",
		MinEpisodes: 10,
		Episode:     1,
	})
	if len(sn.calls) != 1 || !strings.Contains(sn.calls[0], "megaplay.buzz/stream/s-2/163517/sub?s=tcdn") {
		t.Errorf("sniffer calls = %v", sn.calls)
	}
}

func TestSearchParsesOnlyMainList(t *testing.T) {
	shows := parseResults(string(file(t, "testdata/filter_frieren.html")), domain.Dub)
	var s2 *domain.Show
	for i, s := range shows {
		if s.ID == "8384" {
			s2 = &shows[i]
		}
		if s.ID == "6180" {
			t.Errorf("mini anime has no dub episodes but was returned for dub: %+v", s)
		}
	}
	if s2 == nil || s2.Title != "Frieren: Beyond Journey's End Season 2" || s2.Episodes != 10 || s2.Type != "TV" ||
		len(s2.AltTitles) != 1 || s2.AltTitles[0] != "Sousou no Frieren 2nd Season" {
		t.Fatalf("season 2 = %+v (all: %+v)", s2, shows)
	}
}

func TestStreamsAndIDs(t *testing.T) {
	p, _ := fixtureProvider(t)
	ctx := context.Background()
	_, mal, err := p.ExternalIDs(ctx, "8384")
	if err != nil || mal != 59978 {
		t.Fatalf("mal=%d err=%v", mal, err)
	}
	eps, err := p.Episodes(ctx, "8384", domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := p.Streams(ctx, "8384", eps[0], domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	best, _ := domain.SelectStream(streams, "best")
	if best.Height != 1080 || best.Label != "HD-1 · 1080p" || !best.NeedsProxy || !best.WrappedSegments ||
		best.Headers["Referer"] != "https://megaplay.buzz/" || best.URL != "https://megap.mikora.top/806c/c512/index-f1-v1-a1.m3u8" {
		t.Errorf("best = %+v", best)
	}
	if len(best.Subtitles) != 1 || best.Subtitles[0].Lang != "en" {
		t.Errorf("subtitles = %+v", best.Subtitles)
	}
}

func TestParseServersPreferenceAndMode(t *testing.T) {
	var s struct{ Result string }
	decode(t, string(file(t, "testdata/servers_8384_1.json")), &s)
	dub := parseServers(s.Result, domain.Dub)
	if len(dub) == 0 || dub[0].name != "HD-1" || dub[len(dub)-1].name != "Vidstream-2" {
		t.Fatalf("dub servers = %+v", dub)
	}
}

func decode(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatal(err)
	}
}

func TestSortSubtitles(t *testing.T) {
	subs := []domain.Subtitle{
		{Label: "Arabic", Lang: "ar"},
		{Label: "English (AI)", Lang: "en"},
		{Label: "English", Lang: "en"},
		{Label: "Spanish", Lang: "es"},
	}
	sortSubtitles(subs)
	var got []string
	for _, s := range subs {
		got = append(got, s.Label)
	}
	if strings.Join(got, "|") != "English|Arabic|Spanish|English (AI)" {
		t.Fatalf("order = %v", got)
	}
}
