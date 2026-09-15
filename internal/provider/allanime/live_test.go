//go:build live

package allanime

import (
	"testing"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider/providertest"
)

func TestLive(t *testing.T) {
	providertest.Run(t, New(httpx.New(httpx.Options{}), ""), providertest.Case{
		Query:              "frieren",
		Mode:               domain.Sub,
		ShowTitle:          "Sousou no Frieren",
		MinEpisodes:        28,
		Episode:            1,
		StreamsUnavailable: true,
	})
}
