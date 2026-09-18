package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/auth"
	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/discord"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/mapping"
	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/provider"
	"github.com/EmoFa/tsuzuki/internal/provider/anikoto"
	"github.com/EmoFa/tsuzuki/internal/provider/animepahe"
	"github.com/EmoFa/tsuzuki/internal/provider/senshi"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/streamcheck"
	"github.com/EmoFa/tsuzuki/internal/streamproxy"
	"github.com/EmoFa/tsuzuki/internal/tracker"
	"github.com/EmoFa/tsuzuki/internal/update"
)

// HTTP returns the shared scraping client, with browser-based challenge solving
// and clearances persisted in the database.
func (a *App) HTTP(ctx context.Context) (*httpx.Client, error) {
	if a.http != nil {
		return a.http, nil
	}
	st, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	solver := &browser.Solver{
		BinPath:      a.Config.Browser.Path,
		ProfileDir:   a.browserProfileDir(),
		AutoDownload: a.Config.Browser.AutoDownload,
		DownloadDir:  filepath.Join(a.Paths.CacheDir, "chromium"),
		Headless:     a.Config.Browser.Headless,
		Notify:       a.notify,
	}
	a.http = httpx.New(httpx.Options{Solver: solver, Store: st})
	return a.http, nil
}

// browserProfileDir holds the solver's persistent browser profile.
func (a *App) browserProfileDir() string {
	return filepath.Join(a.Paths.DataDir, "browser-profile")
}

// Providers returns every implemented provider.
func (a *App) Providers(ctx context.Context) (*provider.Registry, error) {
	client, err := a.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	return a.newRegistry(client), nil
}

// newRegistry builds every provider on client.
func (a *App) newRegistry(client *httpx.Client) *provider.Registry {
	return provider.NewRegistry(
		anikoto.New(client, a.Sniffer(), ""),
		senshi.New(client, "", ""),
		animepahe.New(client, ""),
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
	// Development only: point at a fake AniList to test syncing without an account.
	if u := os.Getenv("TSUZUKI_ANILIST_API"); u != "" {
		a.anilist = a.anilist.WithURL(u)
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

func (a *App) aniListClientID() int {
	if id := a.Config.Tracking.AnilistClientID; id != 0 {
		return id
	}
	return DefaultAniListClientID
}

func (a *App) tokenFile() auth.TokenFile {
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
		tok, err := a.tokenFile().Load()
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

// Session builds a watch session wired to the real player, providers and store.
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
	cfg := a.Config
	mpv := player.New(player.Options{MpvPath: cfg.Player.MpvPath, ExtraArgs: cfg.Player.ExtraArgs})
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
			SkipFillerEpisodes: cfg.Skip.FillerEpisodes,
			SkipRecapEpisodes:  cfg.Skip.RecapEpisodes,
			Subtitles:          a.subtitlePrefs(),
		},
		Providers: reg,
		Resolver:  mapper,
		Progress:  st,
		Play: func(ctx context.Context, req player.Request) (session.Playback, error) {
			return mpv.Play(ctx, req)
		},
		Proxy:        func() (session.Proxy, error) { return a.StreamProxy() },
		Check:        checker.Check,
		OnWatched:    a.trackWatched,
		SkipRanges:   aniskip.ranges,
		EpisodeKinds: a.FillerList(client, st).Kinds,
		ShowPrefs:    st.ShowPrefs,
		OnStatus:     a.withPresence(onStatus),
	}), nil
}

// DefaultDiscordClientID is tsuzuki's Discord application, whose name Discord
// shows as "Watching <name>". discord.client_id overrides it.
const DefaultDiscordClientID = "1549464164995563550"

func (a *App) discordClientID() string {
	if id := a.Config.Discord.ClientID; id != "" {
		return id
	}
	return DefaultDiscordClientID
}

// withPresence also sends session updates to Discord when enabled.
func (a *App) withPresence(onStatus func(session.Status)) func(session.Status) {
	presence := a.Presence()
	if presence == nil {
		return onStatus
	}
	obs := &discord.Observer{Presence: presence, ShowCover: a.Config.Discord.ShowCover}
	return func(st session.Status) {
		obs.Status(st)
		if onStatus != nil {
			onStatus(st)
		}
	}
}

// Presence starts the Discord presence updater on first use, or returns nil
// when it's disabled or no application ID is configured.
func (a *App) Presence() *discord.Presence {
	id := a.discordClientID()
	if !a.Config.Discord.Enabled || id == "" {
		return nil
	}
	a.lazyMu.Lock()
	defer a.lazyMu.Unlock()
	if a.presence == nil {
		a.presence = discord.NewPresence(func(ctx context.Context) (discord.Setter, error) {
			return discord.Dial(ctx, id)
		})
		a.presence.Start()
	}
	return a.presence
}

// Sniffer returns the shared headless browser used to run embed players. The
// browser itself starts on first use.
func (a *App) Sniffer() *browser.Sniffer {
	a.lazyMu.Lock()
	defer a.lazyMu.Unlock()
	if a.sniffer == nil {
		a.sniffer = &browser.Sniffer{
			BinPath:      a.Config.Browser.Path,
			AutoDownload: a.Config.Browser.AutoDownload,
			DownloadDir:  filepath.Join(a.Paths.CacheDir, "chromium"),
		}
	}
	return a.sniffer
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

// notify surfaces a message that needs the user's attention: inside the TUI
// when it is running, otherwise on stderr.
func (a *App) notify(msg string) {
	if ch := a.notices; ch != nil {
		select {
		case ch <- msg:
		default: // never block a solver on a busy UI
		}
		return
	}
	fmt.Fprintln(os.Stderr, "»", msg)
}

func (a *App) subtitlePrefs() domain.SubtitlePrefs {
	return domain.SubtitlePrefs{Languages: a.Config.Subtitles.Languages, Show: a.Config.Subtitles.Show}
}

// UpdateStatus describes the latest release compared with this build.
type UpdateStatus struct {
	Current, Latest string
	Available       bool
	Command         string // how to upgrade this installation
}

// CheckUpdate compares this build with the latest release. cache may be nil.
func (a *App) CheckUpdate(ctx context.Context, client *httpx.Client, cache update.Cache) (UpdateStatus, error) {
	s := UpdateStatus{Current: buildinfo.Version}
	r, err := (&update.Checker{Client: client, Cache: cache}).Latest(ctx)
	if err != nil {
		return s, err
	}
	s.Latest, s.Available = r.Version, update.Newer(buildinfo.Version, r.Version)
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		s.Command = update.UpgradeCommand(exe)
	}
	return s, nil
}
