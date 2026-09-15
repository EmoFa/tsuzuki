package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/provider/allanime"
	"github.com/EmoFa/anitui/internal/provider/animepahe"
	"github.com/EmoFa/anitui/internal/provider/senshi"
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

// notify surfaces a message that needs the user's attention. The TUI will
// replace this with an in-app prompt.
func (a *App) notify(msg string) {
	fmt.Fprintln(os.Stderr, "»", msg)
}
