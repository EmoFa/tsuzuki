//go:build live

package kickassanime

import (
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider/providertest"
)

func TestLive(t *testing.T) {
	for _, mode := range []domain.Mode{domain.Sub, domain.Dub} {
		t.Run(string(mode), func(t *testing.T) {
			providertest.Run(t, New(httpx.New(httpx.Options{}), ""), providertest.Case{
				Query:       "frieren",
				Mode:        mode,
				ShowTitle:   "Frieren: Beyond Journey's End",
				MinEpisodes: 28,
				Episode:     1,
			})
		})
	}
}
