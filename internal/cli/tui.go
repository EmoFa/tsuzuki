package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tui"
)

// runTUI starts the full-screen interface.
func runTUI(ctx context.Context, app *App) error {
	st, err := app.Store(ctx)
	if err != nil {
		return err
	}
	al, err := app.AniList(ctx)
	if err != nil {
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
	return tui.Run(ctx, &tuiServices{app: app, store: st, anilist: al}, notices)
}

// tuiServices adapts the app to what the TUI needs.
type tuiServices struct {
	app     *App
	store   *store.Store
	anilist *anilist.Client
}

func (s *tuiServices) Search(ctx context.Context, query string) ([]anilist.Media, error) {
	return s.anilist.Search(ctx, query, 25)
}

func (s *tuiServices) Media(ctx context.Context, id int) (anilist.Media, error) {
	return s.anilist.Media(ctx, id)
}

func (s *tuiServices) RecentShows(ctx context.Context, limit int) ([]store.Progress, error) {
	return s.store.RecentShows(ctx, limit)
}

func (s *tuiServices) ShowProgress(ctx context.Context, mediaID int) ([]store.Progress, error) {
	return s.store.ShowProgress(ctx, mediaID)
}

func (s *tuiServices) ProviderEpisodes(ctx context.Context, media anilist.Media, mode domain.Mode) ([]domain.Episode, error) {
	reg, err := s.app.Providers(ctx)
	if err != nil {
		return nil, err
	}
	mapper, err := s.app.Mapper(ctx)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, name := range s.app.Config.Providers.Order {
		p, err := reg.Get(name)
		if err != nil {
			continue
		}
		show, err := mapper.Resolve(ctx, p, media, mode)
		if err == nil {
			var eps []domain.Episode
			if eps, err = p.Episodes(ctx, show.ID, mode); err == nil && len(eps) > 0 {
				return eps, nil
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return nil, fmt.Errorf("no provider lists episodes for %s: %w", media.DisplayTitle(), errors.Join(errs...))
}

func (s *tuiServices) Watch(ctx context.Context, req session.Request, onStatus func(session.Status)) error {
	// A session per watch keeps each watch's status callback separate.
	sess, err := s.app.Session(ctx, onStatus)
	if err != nil {
		return err
	}
	return sess.Watch(ctx, req)
}

func (s *tuiServices) ListEntries(ctx context.Context, status string) ([]store.ListEntry, error) {
	return s.store.ListEntries(ctx, status)
}

func (s *tuiServices) ListEntry(ctx context.Context, mediaID int) (*store.ListEntry, error) {
	return s.store.ListEntry(ctx, mediaID)
}

func (s *tuiServices) SetListStatus(ctx context.Context, mediaID int, status string) (string, error) {
	t, err := s.app.Tracker(ctx)
	if err != nil {
		return "", err
	}
	r, err := t.SetStatus(ctx, mediaID, status)
	if err != nil {
		return "", err
	}
	label := StatusLabel(r.Entry.Status)
	switch {
	case !r.Changed:
		return "Already " + label + ".", nil
	case r.Synced:
		return "Set to " + label + " on AniList.", nil
	case r.SyncErr != nil:
		return "Set to " + label + "; AniList sync pending: " + firstLineOf(r.SyncErr.Error()), nil
	}
	return "Set to " + label + ".", nil
}

func (s *tuiServices) Account() tui.Account {
	a := tui.Account{Backend: s.app.Config.Tracking.Backend}
	if tok, err := s.app.tokenFile().Load(); err == nil && tok != nil {
		a.User = tok.UserName
		a.LoggedIn = tok.Valid()
		a.Expired = !tok.Valid()
	}
	return a
}

func (s *tuiServices) Login(ctx context.Context) (string, error) {
	return login(ctx, s.app, func(url string) {
		s.app.notify("If your browser didn't open, visit " + url)
	})
}

func (s *tuiServices) Sync(ctx context.Context) (string, error) {
	return syncList(ctx, s.app)
}

func (s *tuiServices) EpisodeKinds(ctx context.Context, media anilist.Media) (map[int]skip.EpisodeKind, error) {
	client, err := s.app.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	return s.app.FillerList(client, s.store).Kinds(ctx, media)
}

func (s *tuiServices) Settings() tui.Settings {
	return tui.Settings{
		Version:    buildinfo.Version,
		Config:     s.app.Config,
		ConfigPath: s.app.ConfigPath,
		DataDir:    s.app.Paths.DataDir,
		CacheDir:   s.app.Paths.CacheDir,
	}
}

func (s *tuiServices) ShowPrefs(ctx context.Context, mediaID int) (*store.ShowPrefs, error) {
	return s.store.ShowPrefs(ctx, mediaID)
}

func (s *tuiServices) SaveShowPrefs(ctx context.Context, p store.ShowPrefs) error {
	return s.store.SaveShowPrefs(ctx, p)
}

func (s *tuiServices) DeleteShowPrefs(ctx context.Context, mediaID int) error {
	return s.store.DeleteShowPrefs(ctx, mediaID)
}

func (s *tuiServices) CheckUpdate(ctx context.Context, firstNotice bool) (tui.Update, error) {
	if !s.app.Config.General.CheckUpdates {
		return tui.Update{}, nil
	}
	client, err := s.app.HTTP(ctx)
	if err != nil {
		return tui.Update{}, err
	}
	st, err := s.app.CheckUpdate(ctx, client, s.store)
	u := tui.Update{Current: st.Current, Latest: st.Latest, Available: st.Available, Command: st.Command}
	if err != nil || !u.Available || !firstNotice {
		return u, err
	}
	const notifiedKey = "update:notified"
	if last, _, ok, _ := s.store.GetKV(ctx, notifiedKey); !ok || last != u.Latest {
		u.Notify = true
		if err := s.store.PutKV(ctx, notifiedKey, u.Latest); err != nil {
			slog.Warn("remembering update notice", "err", err)
		}
	}
	return u, nil
}
