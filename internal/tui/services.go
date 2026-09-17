// Package tui is tsuzuki's full-screen terminal interface.
package tui

import (
	"context"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
)

// Services is everything the TUI needs from the rest of tsuzuki. Screens call it
// only from tea.Cmds, never from Update or View.
type Services interface {
	Search(ctx context.Context, query string) ([]anilist.Media, error)
	Browse(ctx context.Context, q anilist.BrowseQuery) (anilist.BrowsePage, error)
	Genres(ctx context.Context) ([]string, error)
	Media(ctx context.Context, id int) (anilist.Media, error)
	RecentShows(ctx context.Context, limit int) ([]store.Progress, error)
	ShowProgress(ctx context.Context, mediaID int) ([]store.Progress, error)
	// ProviderEpisodes lists episodes from the first provider that has the show.
	// Used when AniList doesn't know how many episodes have aired.
	ProviderEpisodes(ctx context.Context, media anilist.Media, mode domain.Mode) ([]domain.Episode, error)
	// Watch blocks until playback ends; onStatus may be called from any goroutine.
	Watch(ctx context.Context, req session.Request, onStatus func(session.Status)) error
	Settings() Settings

	// ListEntries returns list entries with status (all when empty).
	ListEntries(ctx context.Context, status string) ([]store.ListEntry, error)
	ListEntry(ctx context.Context, mediaID int) (*store.ListEntry, error)
	// SetListStatus changes a show's list status, returning a note for the user.
	SetListStatus(ctx context.Context, mediaID int, status string) (string, error)
	Account() Account
	// Login runs the AniList browser login, returning the user name.
	Login(ctx context.Context) (string, error)
	// Sync sends pending list changes and fetches the AniList list.
	Sync(ctx context.Context) (string, error)
	// EpisodeKinds classifies episodes (canon, filler, ...); nil when unknown.
	EpisodeKinds(ctx context.Context, media anilist.Media) (map[int]skip.EpisodeKind, error)

	// ShowPrefs returns a show's own settings, or nil when it uses the config.
	ShowPrefs(ctx context.Context, mediaID int) (*store.ShowPrefs, error)
	SaveShowPrefs(ctx context.Context, p store.ShowPrefs) error
	DeleteShowPrefs(ctx context.Context, mediaID int) error

	// CheckUpdate looks for a newer release (at most daily). With firstNotice,
	// Update.Notify is true only the first time a given release is reported.
	CheckUpdate(ctx context.Context, firstNotice bool) (Update, error)
}

// Update describes a newer release, if any.
type Update struct {
	Current, Latest string
	Available       bool
	Command         string
	Notify          bool
}

// Account describes the AniList login.
type Account struct {
	Backend  string // tracking.backend
	User     string
	LoggedIn bool
	Expired  bool
}

// Syncs reports whether list changes are sent to AniList.
func (a Account) Syncs() bool { return a.Backend == "anilist" && a.LoggedIn }

// Settings is what the settings screen shows.
type Settings struct {
	Version    string
	Config     config.Config
	ConfigPath string
	DataDir    string
	CacheDir   string
}
