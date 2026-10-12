package mobile

import (
	"fmt"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/episodes"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

// homeItem is a show on the home screen, with its progress (continue
// watching) or list entry (list tabs).
type homeItem struct {
	Media    anilist.Media
	Progress *store.Progress  `json:",omitempty"`
	Entry    *store.ListEntry `json:",omitempty"`
	CaughtUp bool
}

type homeTab struct {
	Items []homeItem
	// AllFinished: there's history, but every show in it is finished.
	AllFinished bool
}

// recentLimit is how many shows continue watching holds; three times as many
// are read so it still fills up when several have been completed.
const recentLimit = 30

// media looks up a show, falling back to a placeholder when AniList can't be
// reached, so the list still shows.
func (a *App) mediaOrPlaceholder(id int) anilist.Media {
	m, err := a.core.Media(a.ctx, id)
	if err != nil {
		return anilist.Media{ID: id, Title: anilist.Title{Romaji: fmt.Sprintf("AniList #%d", id)}}
	}
	return m
}

// Home returns a home tab: continue watching for an empty status, otherwise
// the list entries with that status (CURRENT, PLANNING, ...).
func (a *App) Home(status string) (string, error) {
	var tab homeTab
	if status == "" {
		recent, err := a.core.RecentShows(a.ctx, 3*recentLimit)
		if err != nil {
			return "", err
		}
		finished, err := a.core.ListEntries(a.ctx, tracker.Completed)
		if err != nil {
			return "", err
		}
		shows, allFinished := episodes.ContinueWatching(recent, finished, recentLimit)
		tab.AllFinished = allFinished
		for i := range shows {
			m := a.mediaOrPlaceholder(shows[i].MediaID)
			tab.Items = append(tab.Items, homeItem{Media: m, Progress: &shows[i], CaughtUp: episodes.CaughtUp(m, &shows[i], nil)})
		}
		return encode(tab, nil)
	}
	entries, err := a.core.ListEntries(a.ctx, status)
	if err != nil {
		return "", err
	}
	for i := range entries {
		m := a.mediaOrPlaceholder(entries[i].MediaID)
		tab.Items = append(tab.Items, homeItem{Media: m, Entry: &entries[i], CaughtUp: episodes.CaughtUp(m, nil, &entries[i])})
	}
	return encode(tab, nil)
}
