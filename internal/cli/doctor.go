package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/core"
	"github.com/EmoFa/tsuzuki/internal/discord"
	"github.com/EmoFa/tsuzuki/internal/player"
)

// Doctor's checks are the core's, so the Android app's diagnostics agree.
type check = core.Check

func newCheck(name string, status core.CheckStatus, detail string) check {
	return check{Name: name, Status: status, Detail: detail}
}

const (
	statusOK   = core.CheckOK
	statusWarn = core.CheckWarn
	statusFail = core.CheckFail
)

func newDoctorCmd(app *App) *cobra.Command {
	var offline, streams bool
	var query string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that tsuzuki's dependencies, services and providers are working",
		Long: `Checks the config, database, mpv, browser, Discord and AniList login, then
AniList, the skip-time and filler services, and a search on every configured
provider. With --streams it also resolves an episode from each provider and
checks the stream plays. Doctor never opens a verification window: a provider
that needs one is reported so you can verify it by watching something.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			checks := localChecks(ctx, app)
			if !offline {
				checks = append(checks, serviceChecks(ctx, app, timeout)...)
				checks = append(checks, app.ProviderChecks(ctx, query, streams, timeout)...)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			failed := false
			var details []string // multi-line details, shown below the table
			for _, c := range checks {
				mark := map[core.CheckStatus]string{statusOK: "ok", statusWarn: "warn", statusFail: "FAIL"}[c.Status]
				failed = failed || c.Status == statusFail
				first, rest, multi := strings.Cut(c.Detail, "\n")
				fmt.Fprintf(w, "%s\t%s\t%s\n", c.Name, mark, first)
				if multi {
					details = append(details, fmt.Sprintf("\n%s:\n%s", c.Name, rest))
				}
			}
			w.Flush()
			for _, d := range details {
				fmt.Fprintln(cmd.OutOrStdout(), d)
			}
			if failed {
				return errors.New("some checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "only check local setup (no AniList, services or providers)")
	cmd.Flags().BoolVar(&streams, "streams", false, "also resolve and health-check a stream from each provider")
	cmd.Flags().StringVar(&query, "query", "frieren", "title to search providers for")
	cmd.Flags().DurationVar(&timeout, "timeout", 45*time.Second, "time limit per provider or service")
	return cmd
}

func localChecks(ctx context.Context, app *App) []check {
	checks := []check{newCheck("tsuzuki", statusOK, fmt.Sprintf("%s, %s/%s", versionString(), runtime.GOOS, runtime.GOARCH))}
	if _, err := os.Stat(app.ConfigPath); err == nil {
		checks = append(checks, newCheck("config", statusOK, app.ConfigPath))
	} else {
		checks = append(checks, newCheck("config", statusOK, "defaults (no config file at "+app.ConfigPath+")"))
	}

	st, err := app.Store(ctx)
	if err != nil {
		checks = append(checks, newCheck("database", statusFail, err.Error()))
	} else {
		v, _ := st.SchemaVersion(ctx)
		checks = append(checks, newCheck("database", statusOK, fmt.Sprintf("schema v%d, %s", v, app.Paths.Database())))
	}
	checks = append(checks, newCheck("log", statusOK, app.Paths.LogFile()))

	if mpv, err := player.FindMpv(app.Config.Player.MpvPath); err != nil {
		checks = append(checks, newCheck("mpv", statusFail, err.Error()))
	} else {
		checks = append(checks, newCheck("mpv", statusOK, withVersion(ctx, mpv)))
	}

	if bin, err := browser.FindBinary(app.Config.Browser.Path); err != nil {
		c := newCheck("browser", statusFail, err.Error()+" (needed for Anikoto and bot checks)")
		if app.Config.Browser.AutoDownload {
			c.Status, c.Detail = statusWarn, err.Error()+" (will be downloaded on first use)"
		}
		checks = append(checks, c)
	} else {
		checks = append(checks, newCheck("browser", statusOK, withVersion(ctx, bin)))
	}

	for _, name := range app.Config.DroppedProviders() {
		checks = append(checks, newCheck("provider "+name, statusWarn,
			fmt.Sprintf("no longer supported: %s. Remove it from providers.order.", config.RemovedProviders[name])))
	}
	checks = append(checks, discordCheck(ctx, app), accountCheck(app))
	if st != nil {
		if pending, err := st.PendingSyncs(ctx); err == nil && len(pending) > 0 {
			checks = append(checks, newCheck("sync queue", statusWarn, fmt.Sprintf("%d list change(s) not sent to AniList yet; last error: %s (run `tsuzuki sync`)",
				len(pending), core.FirstLine(pending[len(pending)-1].LastError))))
		}
		if list, err := st.Clearances(ctx); err == nil {
			detail := "none stored"
			if len(list) > 0 {
				var hosts []string
				for _, c := range list {
					hosts = append(hosts, fmt.Sprintf("%s (%s)", c.Host, c.ObtainedAt.Format("2006-01-02")))
				}
				detail = strings.Join(hosts, ", ")
			}
			checks = append(checks, newCheck("clearances", statusOK, detail))
		}
	}
	return checks
}

// withVersion appends the first line of `bin --version`, when it answers quickly.
func withVersion(ctx context.Context, bin string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return bin
	}
	line, _, _ := strings.Cut(core.FirstLine(string(out)), " Copyright")
	line = strings.TrimSpace(line)
	if line == "" {
		return bin
	}
	return fmt.Sprintf("%s (%s)", bin, line)
}

func accountCheck(app *App) check {
	backend := app.Config.Tracking.Backend
	tok, err := app.TokenFile().Load()
	switch {
	case err != nil:
		return newCheck("anilist login", statusFail, err.Error())
	case backend == "local" && tok.Valid():
		return newCheck("anilist login", statusOK, "logged in as "+tok.UserName+", but tracking.backend is local (not syncing)")
	case backend == "local":
		return newCheck("anilist login", statusOK, "not used (tracking.backend = local)")
	case tok == nil:
		return newCheck("anilist login", statusWarn, "not logged in: progress is kept locally until you run `tsuzuki login`")
	case !tok.Valid():
		return newCheck("anilist login", statusWarn, "login for "+tok.UserName+" expired: run `tsuzuki login`")
	case !tok.ExpiresAt.IsZero() && time.Until(tok.ExpiresAt) < 14*24*time.Hour:
		return newCheck("anilist login", statusWarn, fmt.Sprintf("%s, expires %s: run `tsuzuki login` soon", tok.UserName, tok.ExpiresAt.Format("2006-01-02")))
	}
	detail := "logged in as " + tok.UserName
	if !tok.ExpiresAt.IsZero() {
		detail += " until " + tok.ExpiresAt.Format("2006-01-02")
	}
	return newCheck("anilist login", statusOK, detail)
}

// serviceChecks reaches AniList (verifying the login), AniSkip, the filler
// list and GitHub's releases.
func serviceChecks(ctx context.Context, app *App, timeout time.Duration) []check {
	client, err := app.HTTP(ctx)
	if err != nil {
		return []check{newCheck("services", statusFail, err.Error())}
	}
	update := core.Probe{Name: "update", Run: func(ctx context.Context) (string, error) {
		if !app.Config.General.CheckUpdates {
			return "checks off (general.check_updates = false)", nil
		}
		st, err := app.CheckUpdate(ctx, client, nil)
		switch {
		case err != nil:
			return "", err
		case st.Available:
			return "", core.Warning(fmt.Sprintf("%s is available (you have %s): %s", st.Latest, st.Current, st.Command))
		case st.Dev:
			return "dev build (latest release " + st.Latest + ")", nil
		}
		return "up to date (" + st.Latest + ")", nil
	}}
	// After the AniList check, where doctor always showed it.
	return core.RunProbes(ctx, timeout, slices.Insert(app.ServiceProbes(), 1, update))
}

func discordCheck(ctx context.Context, app *App) check {
	id := app.discordClientID()
	switch {
	case !app.Config.Discord.Enabled:
		return newCheck("discord", statusOK, "disabled")
	case id == "":
		return newCheck("discord", statusWarn, "no application ID: set discord.client_id to show presence")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := discord.Dial(ctx, id)
	switch {
	case errors.Is(err, discord.ErrNotRunning):
		return newCheck("discord", statusWarn, "Discord isn't running (presence starts when it is)")
	case err != nil:
		return newCheck("discord", statusFail, err.Error())
	}
	defer c.Close()
	who := c.User.Global
	if who == "" {
		who = c.User.Username
	}
	if who == "" {
		return newCheck("discord", statusOK, "connected")
	}
	return newCheck("discord", statusOK, "connected as "+who)
}
