package allanime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
)

// fixtureProvider serves fixtures chosen by which query the request body carries.
func fixtureProvider(t *testing.T) *Provider {
	t.Helper()
	search, _ := os.ReadFile("testdata/search_frieren.json")
	episodes, _ := os.ReadFile("testdata/episodes_frieren.json")
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"api.allanime.day/api": func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST (GET is challenged)", r.Method)
			}
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			switch {
			case strings.Contains(body.Query, "shows("):
				return providertest.Respond(r, 200, "application/json", search), nil
			case strings.Contains(body.Query, "show("):
				return providertest.Respond(r, 200, "application/json", episodes), nil
			}
			t.Errorf("unexpected query %q", body.Query)
			return providertest.Respond(r, 400, "text/plain", nil), nil
		},
	}}
	return New(httpx.New(httpx.Options{Transport: routes}), "")
}

func TestContractFixtures(t *testing.T) {
	providertest.Run(t, fixtureProvider(t), providertest.Case{
		Query:              "frieren",
		Mode:               domain.Sub,
		ShowTitle:          "Sousou no Frieren",
		MinEpisodes:        28,
		Episode:            1,
		StreamsUnavailable: true,
	})
}

func TestSearchCarriesExternalIDs(t *testing.T) {
	shows, err := fixtureProvider(t).Search(context.Background(), "frieren", domain.Sub)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range shows {
		if s.ID == "qpeexkeTa7DzLjRnp" {
			if s.AniListID != 182255 || s.MalID != 59978 || len(s.AltTitles) == 0 {
				t.Fatalf("season 2 = %+v", s)
			}
			return
		}
	}
	t.Fatal("season 2 not in results")
}

func TestAPIErrorsSurface(t *testing.T) {
	body, _ := os.ReadFile("testdata/episode_crypto_missing.json")
	routes := providertest.Routes{T: t, Routes: map[string]func(*http.Request) (*http.Response, error){
		"api.allanime.day/api": func(r *http.Request) (*http.Response, error) {
			return providertest.Respond(r, 200, "application/json", body), nil
		},
	}}
	p := New(httpx.New(httpx.Options{Transport: routes}), "")
	var out struct{}
	err := p.query(context.Background(), "query { x }", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "AA_CRYPTO_MISSING") {
		t.Fatalf("err = %v", err)
	}
}
