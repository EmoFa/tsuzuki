//go:build live

package anikoto

import (
	"testing"

	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider/providertest"
)

// TestLive needs Chrome/Chromium: stream URLs come from megaplay's player.
func TestLive(t *testing.T) {
	sn := &browser.Sniffer{}
	defer sn.Close()
	p := New(httpx.New(httpx.Options{}), sn, "")
	for _, mode := range []domain.Mode{domain.Sub, domain.Dub} {
		t.Run(string(mode), func(t *testing.T) {
			providertest.Run(t, p, providertest.Case{
				Query:       "frieren",
				Mode:        mode,
				ShowTitle:   "Frieren: Beyond Journey's End Season 2",
				MinEpisodes: 10,
				Episode:     2,
			})
		})
	}
}
