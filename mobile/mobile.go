// Package mobile is tsuzuki's API for the Android app, exported with
// `gomobile bind`. gomobile only passes simple types across, so structured
// values travel as JSON: AniList types use AniList's field names, and store
// types use their Go field names, with durations in nanoseconds.
//
// Every call blocks, so the app makes them off the main thread.
package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/core"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/extractor"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/store"
)

// Host is what the Android app does for tsuzuki.
type Host interface {
	// OpenURL shows a web page, for the AniList login.
	OpenURL(url string) error
	// Play opens the player on requestJSON, a player.Request, and returns it.
	// The player reports to playback until it ends.
	Play(requestJSON string, playback *Playback) (Player, error)
	// Sniff loads a page in a WebView without showing it, and captures the
	// requests it makes that requestJSON's matches select (see sniffRequest).
	// It returns them as JSON, by match name: {"URL", "Status", "Body"} with
	// Body in base64.
	Sniff(requestJSON string) (string, error)
	// Solve gets past a bot check (Cloudflare, DDoS-Guard) on pageURL in a
	// WebView, showing it when the check needs a tap. It returns the
	// WebView's User-Agent and the site's cookies as JSON:
	// {"UserAgent", "Cookies": [{"Name", "Value"}]}.
	Solve(pageURL string) (string, error)
}

// App is tsuzuki running inside the Android app.
type App struct {
	core   *core.App
	host   Host
	ctx    context.Context
	cancel context.CancelFunc

	loginMu     sync.Mutex
	cancelLogin context.CancelFunc

	updateURL string // where to look for releases; empty means GitHub
}

// NewApp starts tsuzuki with its files under filesDir and cacheDir, the app's
// private directories.
func NewApp(filesDir, cacheDir string, host Host) (*App, error) {
	paths := config.Paths{
		ConfigDir: filepath.Join(filesDir, "config"),
		DataDir:   filesDir,
		CacheDir:  cacheDir,
	}
	for _, dir := range []string{paths.ConfigDir, paths.DataDir, paths.CacheDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	cfg, _, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		core: &core.App{
			Paths:      paths,
			ConfigPath: paths.ConfigFile(),
			Config:     cfg,
		},
		host:   host,
		ctx:    ctx,
		cancel: cancel,
	}
	a.core.Platform = core.Platform{
		OpenURL:    host.OpenURL,
		Play:       a.play,
		Solver:     func() httpx.Solver { return hostSolver{host} },
		Sniffer:    func() extractor.Sniffer { return hostSniffer{host} },
		SkipButton: true,
	}
	// Built once here, before the app calls in from several threads.
	if _, err := a.core.AniList(ctx); err != nil {
		cancel()
		return nil, err
	}
	if _, err := a.core.Mapper(ctx); err != nil {
		cancel()
		return nil, err
	}
	return a, nil
}

// Close stops everything tsuzuki started and cancels calls in flight.
func (a *App) Close() {
	a.cancel()
	a.core.Close()
}

// encode marshals v for the app.
func encode(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	return string(b), err
}

func decode[T any](s string) (T, error) {
	var v T
	err := json.Unmarshal([]byte(s), &v)
	return v, err
}

// Search returns AniList shows matching query, as a JSON array.
func (a *App) Search(query string) (string, error) {
	return encode(a.core.Search(a.ctx, query))
}

// Browse takes an anilist.BrowseQuery and returns a page of shows.
func (a *App) Browse(queryJSON string) (string, error) {
	q, err := decode[anilist.BrowseQuery](queryJSON)
	if err != nil {
		return "", err
	}
	return encode(a.core.Browse(a.ctx, q))
}

func (a *App) Genres() (string, error) {
	return encode(a.core.Genres(a.ctx))
}

func (a *App) Media(id int) (string, error) {
	return encode(a.core.Media(a.ctx, id))
}

func (a *App) Sequels(mediaID int) (string, error) {
	return encode(a.core.Sequels(a.ctx, mediaID))
}

func (a *App) Prequels(mediaID int) (string, error) {
	return encode(a.core.Prequels(a.ctx, mediaID))
}

// RecentShows is the latest progress of each recently watched show, for
// Continue watching.
func (a *App) RecentShows(limit int) (string, error) {
	return encode(a.core.RecentShows(a.ctx, limit))
}

func (a *App) ShowProgress(mediaID int) (string, error) {
	return encode(a.core.ShowProgress(a.ctx, mediaID))
}

// ProviderEpisodes lists a show's episodes from the first provider that has
// it, for when AniList doesn't know how many have aired. mode is "sub" or "dub".
func (a *App) ProviderEpisodes(mediaJSON, mode string) (string, error) {
	media, err := decode[anilist.Media](mediaJSON)
	if err != nil {
		return "", err
	}
	return encode(a.core.ProviderEpisodes(a.ctx, media, domain.Mode(mode)))
}

// EpisodeKinds maps episode numbers to canon, filler, mixed or recap; null
// when unknown.
func (a *App) EpisodeKinds(mediaJSON string) (string, error) {
	media, err := decode[anilist.Media](mediaJSON)
	if err != nil {
		return "", err
	}
	return encode(a.core.EpisodeKinds(a.ctx, media))
}

// ListEntries returns list entries with status, or all of them when it's empty.
func (a *App) ListEntries(status string) (string, error) {
	return encode(a.core.ListEntries(a.ctx, status))
}

// ListEntry returns a show's list entry, or null.
func (a *App) ListEntry(mediaID int) (string, error) {
	return encode(a.core.ListEntry(a.ctx, mediaID))
}

// SetListStatus changes a show's list status, returning a note for the user.
func (a *App) SetListStatus(mediaID int, status string) (string, error) {
	return a.core.SetListStatus(a.ctx, mediaID, status)
}

// SetScore rates a show from 1 to 10, returning a note.
func (a *App) SetScore(mediaID int, score float64) (string, error) {
	return a.core.SetScore(a.ctx, mediaID, score)
}

// SetWatched marks episodes from..to watched or not, returning a note.
func (a *App) SetWatched(mediaJSON string, from, to float64, watched bool) (string, error) {
	media, err := decode[anilist.Media](mediaJSON)
	if err != nil {
		return "", err
	}
	return a.core.SetWatched(a.ctx, media, from, to, watched)
}

// StartRewatch begins another watch-through, returning a note.
func (a *App) StartRewatch(mediaID int) (string, error) {
	rw, err := a.core.StartRewatch(a.ctx, mediaID)
	if err != nil {
		return "", err
	}
	return rw.Note(), nil
}

// WatchedBefore maps episodes completed in earlier watch-throughs to true.
func (a *App) WatchedBefore(mediaID int) (string, error) {
	return encode(a.core.WatchedBefore(a.ctx, mediaID))
}

// Round returns {"round": n, "watchedBefore": n} for a show.
func (a *App) Round(mediaID int) (string, error) {
	round, before, err := a.core.Round(a.ctx, mediaID)
	return encode(map[string]int{"round": round, "watchedBefore": before}, err)
}

func (a *App) ShowPrefs(mediaID int) (string, error) {
	return encode(a.core.ShowPrefs(a.ctx, mediaID))
}

// SaveShowPrefs takes a store.ShowPrefs. Subtitle languages must be codes
// like "en".
func (a *App) SaveShowPrefs(prefsJSON string) error {
	p, err := decode[store.ShowPrefs](prefsJSON)
	if err != nil {
		return err
	}
	for _, l := range p.SubLanguages {
		if !config.IsLanguageCode(l) {
			return fmt.Errorf("%q isn't a language code like en or es", l)
		}
	}
	return a.core.SaveShowPrefs(a.ctx, p)
}

func (a *App) DeleteShowPrefs(mediaID int) error {
	return a.core.DeleteShowPrefs(a.ctx, mediaID)
}

// Account describes the AniList login.
func (a *App) Account() (string, error) {
	return encode(a.core.Account(), nil)
}

// Login runs the AniList login through Host.OpenURL, returning the user name.
// It waits until the login completes, fails, or CancelLogin is called.
func (a *App) Login() (string, error) {
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	a.loginMu.Lock()
	a.cancelLogin = cancel
	a.loginMu.Unlock()
	return a.core.Login(ctx, func(string) {})
}

// CancelLogin gives up on a login in progress, as when the user closes the
// login page.
func (a *App) CancelLogin() {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.cancelLogin != nil {
		a.cancelLogin()
	}
}

func (a *App) Logout() error {
	return a.core.Logout()
}

// Sync sends pending list changes to AniList and fetches the list.
func (a *App) Sync() (string, error) {
	return a.core.Sync(a.ctx)
}

// Config returns the settings, as config.Config.
func (a *App) Config() (string, error) {
	return encode(a.core.Config, nil)
}

// DiscoverLists are the lists Discover offers (this season, trending, ...),
// each with its anilist.BrowseQuery.
func (a *App) DiscoverLists() (string, error) {
	return encode(anilist.DiscoverLists(time.Now()), nil)
}

// SetConfig changes the settings: configJSON is a config.Config, or any part
// of one, applied over the current settings. Invalid settings are refused
// with an error saying what's wrong.
func (a *App) SetConfig(configJSON string) error {
	cfg := a.core.Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return err
	}
	return a.core.SetConfig(cfg)
}

// ConfigOptions lists the values settings can take, for the app's pickers.
func (a *App) ConfigOptions() (string, error) {
	return encode(map[string][]string{
		"Modes":       config.Modes,
		"Qualities":   config.Qualities,
		"Providers":   config.ProviderNames,
		"Backends":    config.Backends,
		"SkipActions": config.SkipActions,
	}, nil)
}

// Version is this build's version.
func (a *App) Version() string { return buildinfo.Version }
