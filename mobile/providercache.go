package mobile

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/core"
	"github.com/EmoFa/tsuzuki/internal/domain"
)

// providerEpisodesTTL keeps reopening a show from asking the providers again.
const providerEpisodesTTL = time.Hour

// providerEpisodeCache remembers providers' episode lists in memory, per show
// and mode. Failures aren't kept, so they're retried.
type providerEpisodeCache struct {
	mu      sync.Mutex
	entries map[string]cachedProviderEpisodes
}

type cachedProviderEpisodes struct {
	eps []domain.Episode
	at  time.Time
}

func (c *providerEpisodeCache) get(ctx context.Context, app *core.App, media anilist.Media, mode domain.Mode) ([]domain.Episode, error) {
	key := strconv.Itoa(media.ID) + "/" + string(mode)
	c.mu.Lock()
	hit, ok := c.entries[key]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < providerEpisodesTTL {
		return hit.eps, nil
	}
	eps, err := app.ProviderEpisodes(ctx, media, mode)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cachedProviderEpisodes{}
	}
	c.entries[key] = cachedProviderEpisodes{eps, time.Now()}
	c.mu.Unlock()
	return eps, nil
}

// missingThumbnails reports that AniList lacks a thumbnail for some aired episode.
func missingThumbnails(media anilist.Media, listed map[float64]anilist.EpisodeInfo) bool {
	for i := 1; i <= media.AiredEpisodes(); i++ {
		if listed[float64(i)].Thumbnail == "" {
			return true
		}
	}
	return false
}
