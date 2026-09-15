//go:build live

package allanime

import (
	"testing"

	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider/providertest"
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
