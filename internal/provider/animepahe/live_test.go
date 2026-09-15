//go:build live

package animepahe

import (
	"context"
	"errors"
	"testing"

	"github.com/EmoFa/anitui/internal/config"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
	"github.com/EmoFa/anitui/internal/store"
)

// TestLive uses the clearance stored by a normal anitui run. It never opens a
// browser: if Cloudflare challenges, it skips.
func TestLive(t *testing.T) {
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), paths.Database())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p := New(httpx.New(httpx.Options{Store: st}), "")

	if _, err := p.Search(context.Background(), "frieren", domain.Sub); err != nil {
		var ce *httpx.ChallengeError
		if errors.As(err, &ce) {
			t.Skip("no valid Cloudflare clearance; run `anitui debug search animepahe frieren` first")
		}
		t.Fatal(err)
	}
	providertest.Run(t, p, providertest.Case{
		Query:       "frieren",
		Mode:        domain.Sub,
		ShowTitle:   "Frieren: Beyond Journey's End",
		MinEpisodes: 28,
		Episode:     1,
	})
}
