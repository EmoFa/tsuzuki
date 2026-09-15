// Package cli wires anitui's commands together.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/buildinfo"
	"github.com/EmoFa/anitui/internal/config"
	"github.com/EmoFa/anitui/internal/discord"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/logx"
	"github.com/EmoFa/anitui/internal/mapping"
	"github.com/EmoFa/anitui/internal/store"
	"github.com/EmoFa/anitui/internal/streamproxy"
	"github.com/EmoFa/anitui/internal/tracker"
)

// skipConfigAnnotation marks commands that must work even when the config file
// is invalid (e.g. so the user can fix it with `config edit`).
const skipConfigAnnotation = "anitui/skip-config"

// App is the shared state handed to every command.
type App struct {
	Paths      config.Paths
	ConfigPath string
	Config     config.Config

	store   *store.Store
	http    *httpx.Client
	anilist *anilist.Client
	proxy   *streamproxy.Proxy
	sniffer *browser.Sniffer
	mapper  *mapping.Mapper
	// lazyMu guards the lazily built services below. The TUI calls them from
	// several goroutines, and login replaces the tracker.
	lazyMu    sync.Mutex
	tracker   *tracker.Tracker
	presence  *discord.Presence
	notices   chan<- string // set while the TUI runs
	closeLogs func() error
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

func (a *App) Close() {
	a.lazyMu.Lock()
	presence := a.presence
	a.lazyMu.Unlock()
	if presence != nil {
		presence.Close() // clears the presence before the process exits
	}
	if a.sniffer != nil {
		a.sniffer.Close()
	}
	if a.proxy != nil {
		a.proxy.Close()
	}
	if a.store != nil {
		a.store.Close()
	}
	if a.closeLogs != nil {
		a.closeLogs() //nolint:errcheck // nowhere left to report it
	}
}

func NewRootCmd() (*cobra.Command, *App) {
	app := &App{}
	var debug bool

	root := &cobra.Command{
		Use:           "anitui",
		Short:         "Watch anime from your terminal",
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			paths, err := config.ResolvePaths()
			if err != nil {
				return fmt.Errorf("resolving directories: %w", err)
			}
			app.Paths = paths
			if app.ConfigPath == "" {
				app.ConfigPath = paths.ConfigFile()
			}

			closeLogs, err := logx.Setup(paths.LogFile(), debug)
			if err != nil {
				return fmt.Errorf("opening log file: %w", err)
			}
			app.closeLogs = closeLogs
			slog.Debug("starting", "version", buildinfo.Version, "command", cmd.CommandPath())

			if cmd.Annotations[skipConfigAnnotation] == "" {
				cfg, _, err := config.Load(app.ConfigPath)
				if err != nil {
					return fmt.Errorf("invalid config (fix with `anitui config edit`):\n%w", err)
				}
				app.Config = cfg
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context(), app)
		},
	}
	root.SetVersionTemplate(versionString() + "\n")
	root.PersistentFlags().StringVar(&app.ConfigPath, "config", "", "config file (default is the platform config dir)")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	root.AddCommand(
		newVersionCmd(),
		newConfigCmd(app),
		newSearchCmd(app),
		newWatchCmd(app),
		newContinueCmd(app),
		newHistoryCmd(app),
		newListCmd(app),
		newLoginCmd(app),
		newLogoutCmd(app),
		newWhoamiCmd(app),
		newSyncCmd(app),
		newDoctorCmd(app),
		newDebugCmd(app),
	)
	return root, app
}

func versionString() string {
	s := "anitui " + buildinfo.Version
	if buildinfo.Commit != "" {
		commit := buildinfo.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		// git-describe versions already name the commit.
		if !strings.Contains(buildinfo.Version, commit[:min(7, len(commit))]) {
			s += " (" + commit + ")"
		}
	}
	return s
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print the version",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{skipConfigAnnotation: "true"},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
		},
	}
}
