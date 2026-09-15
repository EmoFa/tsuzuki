// Package cli wires anitui's commands together.
package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/buildinfo"
	"github.com/EmoFa/anitui/internal/config"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/logx"
	"github.com/EmoFa/anitui/internal/store"
)

// skipConfigAnnotation marks commands that must work even when the config file
// is invalid (e.g. so the user can fix it with `config edit`).
const skipConfigAnnotation = "anitui/skip-config"

// App is the shared state handed to every command.
type App struct {
	Paths      config.Paths
	ConfigPath string
	Config     config.Config

	store     *store.Store
	http      *httpx.Client
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
	if a.store != nil {
		a.store.Close()
	}
	if a.closeLogs != nil {
		a.closeLogs()
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
			// The TUI lands here in a later phase.
			fmt.Fprintln(cmd.OutOrStdout(), "The interactive UI isn't built yet. See `anitui --help`.")
			return nil
		},
	}
	root.SetVersionTemplate(versionString() + "\n")
	root.PersistentFlags().StringVar(&app.ConfigPath, "config", "", "config file (default is the platform config dir)")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	root.AddCommand(
		newVersionCmd(),
		newConfigCmd(app),
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
		s += " (" + commit + ")"
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
