// Package core builds tsuzuki's services and holds the operations every
// interface shares. The terminal (internal/cli) and the Android app (mobile)
// supply what differs between them through Platform.
package core

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/auth"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/extractor"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/mapping"
	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/provider"
	"github.com/EmoFa/tsuzuki/internal/provider/anikoto"
	"github.com/EmoFa/tsuzuki/internal/provider/animepahe"
	"github.com/EmoFa/tsuzuki/internal/provider/kickassanime"
	"github.com/EmoFa/tsuzuki/internal/provider/senshi"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/streamcheck"
	"github.com/EmoFa/tsuzuki/internal/streamproxy"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

// Platform is what each interface provides for itself. Every field may be nil.
type Platform struct {
	// Solver gets past bot challenges. Nil leaves them unsolved.
	Solver func() httpx.Solver
	// Sniffer runs embed players in a browser. Nil leaves providers that need
	// one unable to resolve streams.
	Sniffer func() extractor.Sniffer
	// Play starts playing a stream.
	Play func(ctx context.Context, req player.Request) (session.Playback, error)
	// OpenURL shows the AniList login page. Nil opens the system browser.
	OpenURL func(url string) error
	// WatchStatus wraps each watch session's status callback, so the platform
	// can observe playback too (Discord presence).
	WatchStatus func(onStatus func(session.Status)) func(session.Status)
	// SkipButton says the interface shows its own skip button for
	// session.StatusSkipPrompt, instead of the player showing a key to press.
	SkipButton bool
}

// App holds the configuration and the services built from it.
type App struct {
	Paths      config.Paths
	ConfigPath string
	Config     config.Config
	Platform   Platform
	// AniListURL points at another AniList API, for tests and development.
	AniListURL string

	store   *store.Store
	http    *httpx.Client
	anilist *anilist.Client
	proxy   *streamproxy.Proxy
	mapper  *mapping.Mapper
	// lazyMu guards the lazily built services below. Interfaces call them from
	// several goroutines, and login replaces the tracker.
	lazyMu  sync.Mutex
	tracker *tracker.Tracker
}

// Store opens the database on first use.
func (a *App) Store(ctx context.Context) (*store.Store, error) {
	if a.store == nil {
		s, err := store.Open(ctx, a.Paths.Database())
		if err != nil {
			return nil, err
		}
		a.store = s
	}
	return a.store, nil
}

// Close stops the stream proxy and closes the database.
func (a *App) Close() {
	if a.proxy != nil {
		a.proxy.Close()
	}
	if a.store != nil {
		a.store.Close()
	}
}

// HTTP returns the shared scraping client, with the platform's challenge
// solving and clearances persisted in the database.
func (a *App) HTTP(ctx context.Context) (*httpx.Client, error) {
	if a.http != nil {
		return a.http, nil
	}
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	opts := httpx.Options{Store: st}
	if a.Platform.Solver != nil {
		opts.Solver = a.Platform.Solver()
	}
	a.http = httpx.New(opts)
	return a.http, nil
}

// Providers returns every implemented provider.
func (a *App) Providers(ctx context.Context) (*provider.Registry, error) {
	client, err := a.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	return a.NewRegistry(client), nil
}

// NewRegistry builds every provider on client.
func (a *App) NewRegistry(client *httpx.Client) *provider.Registry {
	var sniffer extractor.Sniffer
	if a.Platform.Sniffer != nil {
		sniffer = a.Platform.Sniffer()
	}
	return provider.NewRegistry(
		anikoto.New(client, sniffer, ""),
		senshi.New(client, "", ""),
		animepahe.New(client, ""),
		kickassanime.New(client, ""),
	)
}

// AniList returns the AniList client, caching media details in the database.
func (a *App) AniList(ctx context.Context) (*anilist.Client, error) {
	if a.anilist != nil {
		return a.anilist, nil
	}
	client, err := a.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	a.anilist = anilist.New(client, st)
	if a.AniListURL != "" {
		a.anilist = a.anilist.WithURL(a.AniListURL)
	}
	return a.anilist, nil
}

// Mapper returns the shared AniList-to-provider show mapper.
func (a *App) Mapper(ctx context.Context) (*mapping.Mapper, error) {
	if a.mapper == nil {
		st, err := a.Store(ctx)
		if err != nil {
			return nil, err
		}
		a.mapper = mapping.New(st)
	}
	return a.mapper, nil
}

// DefaultAniListClientID is tsuzuki's registered AniList API client, whose
// redirect URL is auth.RedirectURL. tracking.anilist_client_id overrides it.
const DefaultAniListClientID = 51172

// AniListClientID is the AniList API client logins use.
func (a *App) AniListClientID() int {
	if id := a.Config.Tracking.AnilistClientID; id != 0 {
		return id
	}
	return DefaultAniListClientID
}

// TokenFile is where the AniList login is kept.
func (a *App) TokenFile() auth.TokenFile {
	return auth.TokenFile{Path: filepath.Join(a.Paths.ConfigDir, "anilist-token.json")}
}

// Tracker returns the list tracker, syncing to AniList when the backend is
// "anilist" and the user is logged in.
func (a *App) Tracker(ctx context.Context) (*tracker.Tracker, error) {
	a.lazyMu.Lock()
	defer a.lazyMu.Unlock()
	if a.tracker != nil {
		return a.tracker, nil
	}
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	t := &tracker.Tracker{Store: st}
	if a.Config.Tracking.Backend == "anilist" {
		tok, err := a.TokenFile().Load()
		if err != nil {
			return nil, err
		}
		if tok.Valid() {
			al, err := a.AniList(ctx)
			if err != nil {
				return nil, err
			}
			t.Remote = tracker.AniListRemote{Client: al.WithToken(tok.AccessToken), UserID: tok.UserID}
		}
	}
	a.tracker = t
	return t, nil
}

// resetTracker makes the next Tracker call rebuild it, after the login changes.
func (a *App) resetTracker() {
	a.lazyMu.Lock()
	a.tracker = nil
	a.lazyMu.Unlock()
}

// FillerList returns the animefillerlist.com client.
func (a *App) FillerList(client *httpx.Client, cache skip.Cache) *skip.FillerList {
	return &skip.FillerList{Client: client, Cache: cache}
}

// aniSkipLookup adapts AniSkip to the session's SkipRanges hook.
type aniSkipLookup struct{ *skip.AniSkip }

func (l aniSkipLookup) ranges(ctx context.Context, media anilist.Media, episode float64, length time.Duration) ([]domain.SkipRange, error) {
	if episode != math.Floor(episode) {
		return nil, nil // AniSkip only knows whole episodes
	}
	return l.Ranges(ctx, media.IDMal, int(episode), length)
}

// trackWatched is the session's OnWatched hook.
func (a *App) trackWatched(ctx context.Context, media anilist.Media, episode float64) (string, error) {
	t, err := a.Tracker(ctx)
	if err != nil {
		return "", err
	}
	r, err := t.EpisodeWatched(ctx, media, episode)
	if err != nil {
		return "", err
	}
	if errors.Is(r.SyncErr, anilist.ErrUnauthorized) {
		return "list updated; AniList login expired, run `tsuzuki login` to sync", nil
	}
	return r.Note(), nil
}

// errNoPlayer is returned when the platform can't play anything.
var errNoPlayer = errors.New("no player available")

// Session builds a watch session wired to the platform's player and the
// providers and store.
func (a *App) Session(ctx context.Context, onStatus func(session.Status)) (*session.Session, error) {
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	reg, err := a.Providers(ctx)
	if err != nil {
		return nil, err
	}
	mapper, err := a.Mapper(ctx)
	if err != nil {
		return nil, err
	}
	client, err := a.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	checker := &streamcheck.Checker{Client: client}
	aniskip := aniSkipLookup{&skip.AniSkip{Client: client, Cache: st}}
	if a.Platform.WatchStatus != nil {
		onStatus = a.Platform.WatchStatus(onStatus)
	}
	cfg := a.Config
	return session.New(session.Deps{
		Settings: session.Settings{
			ProviderOrder:    cfg.ActiveProviders(),
			Quality:          cfg.General.Quality,
			AutoplayNext:     cfg.General.AutoplayNext,
			WatchedThreshold: cfg.General.WatchedThreshold,
			ResumeRewind:     time.Duration(cfg.General.ResumeRewindSeconds) * time.Second,
			CheckTimeout:     cfg.Providers.HealthCheckTimeout.Duration,
			SkipActions: map[domain.SkipKind]string{
				domain.SkipOpening: cfg.Skip.Opening,
				domain.SkipEnding:  cfg.Skip.Ending,
				domain.SkipRecap:   cfg.Skip.Recap,
			},
			SkipButton:         a.Platform.SkipButton,
			SkipFillerEpisodes: cfg.Skip.FillerEpisodes,
			SkipRecapEpisodes:  cfg.Skip.RecapEpisodes,
			Subtitles:          a.SubtitlePrefs(),
		},
		Providers: reg,
		Resolver:  mapper,
		Progress:  st,
		Play: func(ctx context.Context, req player.Request) (session.Playback, error) {
			if a.Platform.Play == nil {
				return nil, errNoPlayer
			}
			return a.Platform.Play(ctx, req)
		},
		Proxy:        func() (session.Proxy, error) { return a.StreamProxy() },
		Check:        checker.Check,
		OnWatched:    a.trackWatched,
		SkipRanges:   aniskip.ranges,
		EpisodeKinds: a.FillerList(client, st).Kinds,
		ShowPrefs:    st.ShowPrefs,
		OnStatus:     onStatus,
	}), nil
}

// StreamProxy starts the local stream proxy on first use.
func (a *App) StreamProxy() (*streamproxy.Proxy, error) {
	a.lazyMu.Lock()
	defer a.lazyMu.Unlock()
	if a.proxy != nil {
		return a.proxy, nil
	}
	// No overall timeout: the proxy streams long bodies.
	p, err := streamproxy.Start(httpx.New(httpx.Options{Timeout: -1}))
	if err != nil {
		return nil, err
	}
	a.proxy = p
	return p, nil
}

// SubtitlePrefs are the configured subtitle preferences.
func (a *App) SubtitlePrefs() domain.SubtitlePrefs {
	return domain.SubtitlePrefs{Languages: a.Config.Subtitles.Languages, Show: a.Config.Subtitles.Show}
}

// SetConfig saves cfg as the settings and uses it from now on. Watches
// already playing keep the settings they started with.
func (a *App) SetConfig(cfg config.Config) error {
	if err := config.Save(a.ConfigPath, cfg); err != nil {
		return err
	}
	a.lazyMu.Lock()
	a.Config = cfg
	// The tracking backend may have changed.
	a.tracker = nil
	a.lazyMu.Unlock()
	return nil
}

// FirstNotice reports whether release version is new to the user, and
// remembers that it has now been mentioned, so an update is announced once.
func (a *App) FirstNotice(ctx context.Context, version string) bool {
	st, err := a.Store(ctx)
	if err != nil {
		return false
	}
	const notifiedKey = "update:notified"
	if last, _, ok, _ := st.GetKV(ctx, notifiedKey); ok && last == version {
		return false
	}
	if err := st.PutKV(ctx, notifiedKey, version); err != nil {
		slog.Warn("remembering update notice", "err", err)
	}
	return true
}
