//go:build live

package senshi

import (
	"testing"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
)

func TestLive(t *testing.T) {
	for _, mode := range []domain.Mode{domain.Sub, domain.Dub} {
		t.Run(string(mode), func(t *testing.T) {
			providertest.Run(t, New(httpx.New(httpx.Options{}), "", ""), providertest.Case{
				Query:       "jujutsu",
				Mode:        mode,
				ShowTitle:   "Jujutsu Kaisen",
				MinEpisodes: 24,
				Episode:     1,
			})
		})
	}
}
