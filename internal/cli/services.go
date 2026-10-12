package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/discord"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/update"
)

// solver gets past bot challenges with a desktop Chrome, sharing a persistent
// profile so clearances survive restarts.
func (a *App) solver() httpx.Solver {
	return &browser.Solver{
		BinPath:      a.Config.Browser.Path,
		ProfileDir:   a.browserProfileDir(),
		AutoDownload: a.Config.Browser.AutoDownload,
		DownloadDir:  filepath.Join(a.Paths.CacheDir, "chromium"),
		Headless:     a.Config.Browser.Headless,
		Notify:       a.notify,
	}
}

// browserProfileDir holds the solver's persistent browser profile.
func (a *App) browserProfileDir() string {
	return filepath.Join(a.Paths.DataDir, "browser-profile")
}

// play starts mpv.
func (a *App) play(ctx context.Context, req player.Request) (session.Playback, error) {
	mpv := player.New(player.Options{MpvPath: a.Config.Player.MpvPath, ExtraArgs: a.Config.Player.ExtraArgs})
	return mpv.Play(ctx, req)
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

// UpdateStatus describes the latest release compared with this build.
type UpdateStatus struct {
	Current, Latest string
	Available       bool
	Dev             bool   // built from source, so releases say nothing about it
	SelfUpgrade     bool   // `tsuzuki upgrade` can replace this binary itself
	Command         string // how to upgrade this installation
}

// installed describes the running binary, for deciding how to upgrade it.
func installed() update.Install {
	exe, err := os.Executable()
	if err != nil {
		return update.Install{}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return update.Detect(exe)
}

// CheckUpdate compares this build with the latest release. cache may be nil.
func (a *App) CheckUpdate(ctx context.Context, client *httpx.Client, cache update.Cache) (UpdateStatus, error) {
	s := UpdateStatus{Current: buildinfo.Version, Dev: !update.IsRelease(buildinfo.Version)}
	r, err := (&update.Checker{Client: client, Cache: cache, URL: a.updateURL}).Latest(ctx)
	if err != nil {
		return s, err
	}
	s.Latest, s.Available = r.Version, update.Newer(buildinfo.Version, r.Version)
	if in := installed(); in.Exe != "" {
		s.Command = in.Command()
		s.SelfUpgrade = in.SelfUpgrades() && !s.Dev
	}
	return s, nil
}
