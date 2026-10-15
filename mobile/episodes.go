package mobile

import (
	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/episodes"
	"github.com/EmoFa/tsuzuki/internal/store"
)

// episodeList is a show's episodes as the details screen shows them.
type episodeList struct {
	Rows   []episodes.Row
	NextUp float64
	// Round is the watch-through, from 1.
	Round int
	// Loading says the provider's list is still to come (remote was false).
	Loading bool
	// ProviderError says why no provider could list the episodes.
	ProviderError string `json:",omitempty"`
}

// Episodes lists a show's episodes in mode ("sub" or "dub"), with what's been
// watched and what's next. Without remote it only reads what's stored, which is
// quick; with it, it also asks AniList for titles and thumbnails,
// animefillerlist.com for filler and, when
// AniList doesn't know how many episodes have aired, the providers.
func (a *App) Episodes(mediaJSON, mode string, remote bool) (string, error) {
	media, err := decode[anilist.Media](mediaJSON)
	if err != nil {
		return "", err
	}
	ctx := a.ctx
	s := episodes.Show{Media: media, Progress: map[float64]store.Progress{}}
	progress, err := a.core.ShowProgress(ctx, media.ID)
	if err != nil {
		return "", err
	}
	for _, p := range progress {
		s.Progress[p.Episode] = p
	}
	if s.Entry, err = a.core.ListEntry(ctx, media.ID); err != nil {
		return "", err
	}
	if s.Round, s.BeforeThrough, err = a.core.Round(ctx, media.ID); err != nil {
		return "", err
	}
	if s.Round > 1 {
		if s.WatchedBefore, err = a.core.WatchedBefore(ctx, media.ID); err != nil {
			return "", err
		}
	}

	list := episodeList{Round: s.Round}
	needsProvider := episodes.NeedsProviderEpisodes(media)
	if remote {
		// These are extras: the list still works without them.
		s.Kinds, _ = a.core.EpisodeKinds(ctx, media)
		if listed, err := a.core.StreamingEpisodes(ctx, media.ID); err == nil {
			s.Listed = anilist.AlignEpisodes(listed, media.Episodes)
		}
		// Providers also supply thumbnails for the episodes AniList has none for.
		if needsProvider || missingThumbnails(media, s.Listed) {
			eps, err := a.providerEps.get(ctx, a.core, media, domain.Mode(mode))
			if err != nil && needsProvider {
				list.ProviderError = err.Error()
			}
			s.Provider = map[float64]domain.Episode{}
			for _, e := range eps {
				// Without needing the list itself, only what's already there gets details.
				if needsProvider || e.Number <= float64(media.AiredEpisodes()) {
					s.Provider[e.Number] = e
				}
			}
		}
	} else {
		list.Loading = needsProvider
	}
	list.Rows = s.Rows(true)
	list.NextUp = s.NextUp()
	return encode(list, nil)
}
