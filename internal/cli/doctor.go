package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/discord"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/provider"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/streamcheck"
)

type checkStatus int

const (
	statusOK checkStatus = iota
	statusWarn
	statusFail
)

type check struct {
	name   string
	status checkStatus
	detail string
}

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
				checks = append(checks, providerChecks(ctx, app, query, streams, timeout)...)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			failed := false
			var details []string // multi-line details, shown below the table
			for _, c := range checks {
				mark := map[checkStatus]string{statusOK: "ok", statusWarn: "warn", statusFail: "FAIL"}[c.status]
				failed = failed || c.status == statusFail
				first, rest, multi := strings.Cut(c.detail, "\n")
				fmt.Fprintf(w, "%s\t%s\t%s\n", c.name, mark, first)
				if multi {
					details = append(details, fmt.Sprintf("\n%s:\n%s", c.name, rest))
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
	checks := []check{{"tsuzuki", statusOK, fmt.Sprintf("%s, %s/%s", versionString(), runtime.GOOS, runtime.GOARCH)}}
	if _, err := os.Stat(app.ConfigPath); err == nil {
		checks = append(checks, check{"config", statusOK, app.ConfigPath})
	} else {
		checks = append(checks, check{"config", statusOK, "defaults (no config file at " + app.ConfigPath + ")"})
	}

	st, err := app.Store(ctx)
	if err != nil {
		checks = append(checks, check{"database", statusFail, err.Error()})
	} else {
		v, _ := st.SchemaVersion(ctx)
		checks = append(checks, check{"database", statusOK, fmt.Sprintf("schema v%d, %s", v, app.Paths.Database())})
	}
	checks = append(checks, check{"log", statusOK, app.Paths.LogFile()})

	if mpv, err := player.FindMpv(app.Config.Player.MpvPath); err != nil {
		checks = append(checks, check{"mpv", statusFail, err.Error()})
	} else {
		checks = append(checks, check{"mpv", statusOK, withVersion(ctx, mpv)})
	}

	if bin, err := browser.FindBinary(app.Config.Browser.Path); err != nil {
		c := check{"browser", statusFail, err.Error() + " (needed for Anikoto and bot checks)"}
		if app.Config.Browser.AutoDownload {
			c.status, c.detail = statusWarn, err.Error()+" (will be downloaded on first use)"
		}
		checks = append(checks, c)
	} else {
		checks = append(checks, check{"browser", statusOK, withVersion(ctx, bin)})
	}

	for _, name := range app.Config.DroppedProviders() {
		checks = append(checks, check{"provider " + name, statusWarn,
			fmt.Sprintf("no longer supported: %s. Remove it from providers.order.", config.RemovedProviders[name])})
	}
	checks = append(checks, discordCheck(ctx, app), accountCheck(app))
	if st != nil {
		if pending, err := st.PendingSyncs(ctx); err == nil && len(pending) > 0 {
			checks = append(checks, check{"sync queue", statusWarn, fmt.Sprintf("%d list change(s) not sent to AniList yet; last error: %s (run `tsuzuki sync`)",
				len(pending), firstLineOf(pending[len(pending)-1].LastError))})
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
			checks = append(checks, check{"clearances", statusOK, detail})
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
	line, _, _ := strings.Cut(firstLineOf(string(out)), " Copyright")
	line = strings.TrimSpace(line)
	if line == "" {
		return bin
	}
	return fmt.Sprintf("%s (%s)", bin, line)
}

func accountCheck(app *App) check {
	backend := app.Config.Tracking.Backend
	tok, err := app.tokenFile().Load()
	switch {
	case err != nil:
		return check{"anilist login", statusFail, err.Error()}
	case backend == "local" && tok.Valid():
		return check{"anilist login", statusOK, "logged in as " + tok.UserName + ", but tracking.backend is local (not syncing)"}
	case backend == "local":
		return check{"anilist login", statusOK, "not used (tracking.backend = local)"}
	case tok == nil:
		return check{"anilist login", statusWarn, "not logged in: progress is kept locally until you run `tsuzuki login`"}
	case !tok.Valid():
		return check{"anilist login", statusWarn, "login for " + tok.UserName + " expired: run `tsuzuki login`"}
	case !tok.ExpiresAt.IsZero() && time.Until(tok.ExpiresAt) < 14*24*time.Hour:
		return check{"anilist login", statusWarn, fmt.Sprintf("%s, expires %s: run `tsuzuki login` soon", tok.UserName, tok.ExpiresAt.Format("2006-01-02"))}
	}
	detail := "logged in as " + tok.UserName
	if !tok.ExpiresAt.IsZero() {
		detail += " until " + tok.ExpiresAt.Format("2006-01-02")
	}
	return check{"anilist login", statusOK, detail}
}

// serviceChecks reaches AniList (verifying the login), AniSkip and the filler list.
func serviceChecks(ctx context.Context, app *App, timeout time.Duration) []check {
	client, err := app.HTTP(ctx)
	if err != nil {
		return []check{{"services", statusFail, err.Error()}}
	}
	timed := func(name string, f func(ctx context.Context) (string, error)) func() check {
		return func() check {
			ctx, cancel := context.WithTimeout(ctx, min(timeout, 20*time.Second))
			defer cancel()
			start := time.Now()
			detail, err := f(ctx)
			elapsed := time.Since(start).Round(100 * time.Millisecond)
			var upd *updateAvailable
			if errors.As(err, &upd) {
				return check{name, statusWarn, fmt.Sprintf("%s is available (you have %s): %s", upd.Latest, upd.Current, upd.Command)}
			}
			if err != nil {
				return check{name, statusFail, fmt.Sprintf("%s · %s", firstLineOf(err.Error()), elapsed)}
			}
			return check{name, statusOK, fmt.Sprintf("%s · %s", detail, elapsed)}
		}
	}

	runs := []func() check{
		timed("anilist api", func(ctx context.Context) (string, error) {
			al, err := app.AniList(ctx)
			if err != nil {
				return "", err
			}
			if err := al.Ping(ctx); err != nil {
				return "", err
			}
			tok, _ := app.tokenFile().Load()
			if !tok.Valid() {
				return "reachable", nil
			}
			user, err := al.WithToken(tok.AccessToken).Viewer(ctx)
			if errors.Is(err, anilist.ErrUnauthorized) {
				return "", errors.New("reachable, but AniList rejected your login: run `tsuzuki login`")
			}
			if err != nil {
				return "", err
			}
			return "reachable, login accepted for " + user.Name, nil
		}),
		timed("update", func(ctx context.Context) (string, error) {
			if !app.Config.General.CheckUpdates {
				return "checks off (general.check_updates = false)", nil
			}
			st, err := app.CheckUpdate(ctx, client, nil)
			switch {
			case err != nil:
				return "", err
			case st.Available:
				return "", &updateAvailable{st}
			case st.Dev:
				return "dev build (latest release " + st.Latest + ")", nil
			}
			return "up to date (" + st.Latest + ")", nil
		}),
		timed("aniskip", func(ctx context.Context) (string, error) {
			// Frieren episode 1 has well-established skip times.
			ranges, err := (&skip.AniSkip{Client: client}).Ranges(ctx, 52991, 1, 0)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("reachable (%d skip times for a sample episode)", len(ranges)), nil
		}),
		timed("filler list", func(ctx context.Context) (string, error) {
			naruto := anilist.Media{ID: 1735, Title: anilist.Title{English: "Naruto Shippuden", Romaji: "Naruto: Shippuuden"}}
			kinds, err := (&skip.FillerList{Client: client}).Kinds(ctx, naruto)
			if err != nil {
				return "", err
			}
			if len(kinds) == 0 {
				return "", errors.New("reachable, but a sample show couldn't be read (the site may have changed)")
			}
			return fmt.Sprintf("reachable (%d episodes classified for a sample show)", len(kinds)), nil
		}),
	}
	results := make([]check, len(runs))
	var wg sync.WaitGroup
	for i, run := range runs {
		wg.Go(func() { results[i] = run() })
	}
	wg.Wait()
	return results
}

// providerChecks runs every configured provider concurrently.
func providerChecks(ctx context.Context, app *App, query string, streams bool, timeout time.Duration) []check {
	st, err := app.Store(ctx)
	if err != nil {
		return []check{{"providers", statusFail, err.Error()}}
	}
	// Stored clearances but no solver: doctor must not open verification windows.
	client := httpx.New(httpx.Options{Store: st})
	reg := app.newRegistry(client)
	checker := &streamcheck.Checker{Client: client}

	names := app.Config.ActiveProviders()
	results := make([]check, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			results[i] = providerCheck(ctx, reg, name, query, streams, timeout, checker, app)
		})
	}
	wg.Wait()
	return results
}

func providerCheck(ctx context.Context, reg *provider.Registry, name, query string, streams bool, timeout time.Duration, checker *streamcheck.Checker, app *App) check {
	label := "provider " + name
	p, err := reg.Get(name)
	if err != nil {
		return check{label, statusFail, "not implemented"}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r := provider.CheckHealth(ctx, p, provider.HealthOptions{
		Query:       query,
		Mode:        domain.Mode(app.Config.General.Mode),
		Quality:     app.Config.General.Quality,
		Streams:     streams,
		CheckStream: checker.Check,
	})
	elapsed := r.Elapsed.Round(100 * time.Millisecond)
	if r.OK() {
		return check{label, statusOK, fmt.Sprintf("%s · %s", r.Detail, elapsed)}
	}

	msg := firstLineOf(r.Err.Error())
	var ce *httpx.ChallengeError
	switch {
	case errors.As(r.Err, &ce):
		return check{label, statusWarn, fmt.Sprintf("needs %s verification: watch something from %s to verify · %s", ce.Kind, name, elapsed)}
	case r.Limited:
		return check{label, statusWarn, fmt.Sprintf("%s failed: %s · %s", r.Stage, msg, elapsed)}
	case errors.Is(r.Err, context.DeadlineExceeded):
		return check{label, statusFail, fmt.Sprintf("%s timed out after %s", r.Stage, timeout)}
	}
	return check{label, statusFail, fmt.Sprintf("%s failed: %s · %s", r.Stage, msg, elapsed)}
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	if len(line) > 160 {
		line = line[:160] + "…"
	}
	return line
}

func discordCheck(ctx context.Context, app *App) check {
	id := app.discordClientID()
	switch {
	case !app.Config.Discord.Enabled:
		return check{"discord", statusOK, "disabled"}
	case id == "":
		return check{"discord", statusWarn, "no application ID: set discord.client_id to show presence"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := discord.Dial(ctx, id)
	switch {
	case errors.Is(err, discord.ErrNotRunning):
		return check{"discord", statusWarn, "Discord isn't running (presence starts when it is)"}
	case err != nil:
		return check{"discord", statusFail, err.Error()}
	}
	defer c.Close()
	who := c.User.Global
	if who == "" {
		who = c.User.Username
	}
	if who == "" {
		return check{"discord", statusOK, "connected"}
	}
	return check{"discord", statusOK, "connected as " + who}
}

// updateAvailable reports a newer release as a warning rather than a failure.
type updateAvailable struct{ UpdateStatus }

func (u *updateAvailable) Error() string { return "tsuzuki " + u.Latest + " is available" }
