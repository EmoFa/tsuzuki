package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/player"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/streamcheck"
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
	var providers, streams bool
	var query string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that anitui's dependencies and providers are working",
		Long: `Checks the config, database, mpv and browser, then searches every configured
provider. With --streams it also resolves an episode from each provider and
checks the stream plays. Doctor never opens a verification window: a provider
that needs one is reported so you can verify it by watching something.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			checks := localChecks(ctx, app)
			if providers || streams {
				checks = append(checks, providerChecks(ctx, app, query, streams, timeout)...)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			failed := false
			for _, c := range checks {
				mark := map[checkStatus]string{statusOK: "ok", statusWarn: "warn", statusFail: "FAIL"}[c.status]
				failed = failed || c.status == statusFail
				fmt.Fprintf(w, "%s\t%s\t%s\n", c.name, mark, c.detail)
			}
			w.Flush()
			if failed {
				return errors.New("some checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&providers, "providers", true, "search each configured provider")
	cmd.Flags().BoolVar(&streams, "streams", false, "also resolve and health-check a stream from each provider")
	cmd.Flags().StringVar(&query, "query", "frieren", "title to search providers for")
	cmd.Flags().DurationVar(&timeout, "timeout", 45*time.Second, "time limit per provider")
	return cmd
}

func localChecks(ctx context.Context, app *App) []check {
	var checks []check
	if _, err := os.Stat(app.ConfigPath); err == nil {
		checks = append(checks, check{"config", statusOK, app.ConfigPath})
	} else {
		checks = append(checks, check{"config", statusOK, "defaults (no config file)"})
	}

	st, err := app.Store(ctx)
	if err != nil {
		checks = append(checks, check{"database", statusFail, err.Error()})
	} else {
		v, _ := st.SchemaVersion(ctx)
		checks = append(checks, check{"database", statusOK, fmt.Sprintf("schema v%d, %s", v, app.Paths.Database())})
	}

	if mpv, err := player.FindMpv(app.Config.Player.MpvPath); err != nil {
		checks = append(checks, check{"mpv", statusFail, err.Error()})
	} else {
		checks = append(checks, check{"mpv", statusOK, mpv})
	}

	if bin, err := browser.FindBinary(app.Config.Browser.Path); err != nil {
		c := check{"browser", statusFail, err.Error() + " (needed for Anikoto and bot checks)"}
		if app.Config.Browser.AutoDownload {
			c.status, c.detail = statusWarn, err.Error()+" (will be downloaded on first use)"
		}
		checks = append(checks, c)
	} else {
		checks = append(checks, check{"browser", statusOK, bin})
	}

	if st != nil {
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

	names := app.Config.Providers.Order
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
