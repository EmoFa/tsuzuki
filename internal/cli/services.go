package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/mapping"
	"github.com/EmoFa/anitui/internal/player"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/provider/allanime"
	"github.com/EmoFa/anitui/internal/provider/animepahe"
	"github.com/EmoFa/anitui/internal/provider/senshi"
	"github.com/EmoFa/anitui/internal/session"
	"github.com/EmoFa/anitui/internal/streamproxy"
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
		ProfileDir:   filepath.Join(a.Paths.DataDir, "browser-profile"),
		AutoDownload: a.Config.Browser.AutoDownload,
		DownloadDir:  filepath.Join(a.Paths.CacheDir, "chromium"),
		Headless:     a.Config.Browser.Headless,
		Notify:       a.notify,
	}
	a.http = httpx.New(httpx.Options{Solver: solver, Store: st})
	return a.http, nil
}

// Providers returns every implemented provider.
func (a *App) Providers(ctx context.Context) (*provider.Registry, error) {
	client, err := a.HTTP(ctx)
	if err != nil {
		return nil, err
	}
	return provider.NewRegistry(
		senshi.New(client, "", ""),
		animepahe.New(client, ""),
		allanime.New(client, ""),
	), nil
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
	return a.anilist, nil
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
	cfg := a.Config
	mpv := player.New(player.Options{MpvPath: cfg.Player.MpvPath, ExtraArgs: cfg.Player.ExtraArgs})
	return session.New(session.Deps{
		Settings: session.Settings{
			ProviderOrder:    cfg.Providers.Order,
			Quality:          cfg.General.Quality,
			AutoplayNext:     cfg.General.AutoplayNext,
			WatchedThreshold: cfg.General.WatchedThreshold,
			ResumeRewind:     time.Duration(cfg.General.ResumeRewindSeconds) * time.Second,
		},
		Providers: reg,
		Resolver:  mapping.New(st),
		Progress:  st,
		Play: func(ctx context.Context, req player.Request) (session.Playback, error) {
			return mpv.Play(ctx, req)
		},
		Proxy:    func() (session.Proxy, error) { return a.StreamProxy() },
		OnStatus: onStatus,
	}), nil
}

// StreamProxy starts the local stream proxy on first use.
func (a *App) StreamProxy() (*streamproxy.Proxy, error) {
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

// notify surfaces a message that needs the user's attention. The TUI will
// replace this with an in-app prompt.
func (a *App) notify(msg string) {
	fmt.Fprintln(os.Stderr, "»", msg)
}
