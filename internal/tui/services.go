// Package tui is anitui's full-screen terminal interface.
package tui

import (
	"context"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/config"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/session"
	"github.com/EmoFa/anitui/internal/store"
)

// Services is everything the TUI needs from the rest of anitui. Screens call it
// only from tea.Cmds, never from Update or View.
type Services interface {
	Search(ctx context.Context, query string) ([]anilist.Media, error)
	Media(ctx context.Context, id int) (anilist.Media, error)
	RecentShows(ctx context.Context, limit int) ([]store.Progress, error)
	ShowProgress(ctx context.Context, mediaID int) ([]store.Progress, error)
	// ProviderEpisodes lists episodes from the first provider that has the show.
	// Used when AniList doesn't know how many episodes have aired.
	ProviderEpisodes(ctx context.Context, media anilist.Media, mode domain.Mode) ([]domain.Episode, error)
	// Watch blocks until playback ends; onStatus may be called from any goroutine.
	Watch(ctx context.Context, req session.Request, onStatus func(session.Status)) error
	Settings() Settings
}

// Settings is what the settings screen shows.
type Settings struct {
	Config     config.Config
	ConfigPath string
	DataDir    string
	CacheDir   string
}
