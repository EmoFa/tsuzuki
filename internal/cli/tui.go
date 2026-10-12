package cli

import (
	"context"

	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/tui"
)

// runTUI starts the full-screen interface.
func runTUI(ctx context.Context, app *App) error {
	if _, err := app.AniList(ctx); err != nil {
		return err
	}
	// Store, HTTP client, AniList and mapper are built once here, before the
	// UI calls them concurrently.
	if _, err := app.HTTP(ctx); err != nil {
		return err
	}
	if _, err := app.Mapper(ctx); err != nil {
		return err
	}
	// Messages that would otherwise go to stderr are shown inside the UI. The
	// channel is never closed: a background solver may still send after exit,
	// and notify never blocks.
	notices := make(chan string, 8)
	app.notices = notices
	return tui.Run(ctx, &tuiServices{App: app}, notices)
}

// tuiServices adapts the app to what the TUI needs. Most of it is the shared
// core; the rest converts to the TUI's types or is terminal-only.
type tuiServices struct {
	*App
}

func (s *tuiServices) Account() tui.Account { return tui.Account(s.App.Account()) }

func (s *tuiServices) Login(ctx context.Context) (string, error) {
	return s.App.Login(ctx, func(url string) {
		s.notify("If your browser didn't open, visit " + url)
	})
}

func (s *tuiServices) Settings() tui.Settings {
	return tui.Settings{
		Version:    buildinfo.Version,
		Config:     s.Config,
		ConfigPath: s.ConfigPath,
		DataDir:    s.Paths.DataDir,
		CacheDir:   s.Paths.CacheDir,
	}
}

func (s *tuiServices) CheckUpdate(ctx context.Context, firstNotice bool) (tui.Update, error) {
	if !s.Config.General.CheckUpdates {
		return tui.Update{}, nil
	}
	client, err := s.HTTP(ctx)
	if err != nil {
		return tui.Update{}, err
	}
	store, err := s.Store(ctx)
	if err != nil {
		return tui.Update{}, err
	}
	st, err := s.App.CheckUpdate(ctx, client, store)
	u := tui.Update{Current: st.Current, Latest: st.Latest, Available: st.Available, Dev: st.Dev,
		SelfUpgrade: st.SelfUpgrade, Command: st.Command}
	if err != nil || !u.Available || !firstNotice {
		return u, err
	}
	u.Notify = s.FirstNotice(ctx, u.Latest)
	return u, nil
}

func (s *tuiServices) Upgrade(ctx context.Context) (string, error) {
	return upgradeNote(ctx)
}

func (s *tuiServices) StartRewatch(ctx context.Context, mediaID int) (string, error) {
	rw, err := s.App.StartRewatch(ctx, mediaID)
	if err != nil {
		return "", err
	}
	return rw.Note(), nil
}
