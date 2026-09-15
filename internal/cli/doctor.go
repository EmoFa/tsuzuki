package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/browser"
	"github.com/EmoFa/anitui/internal/player"
)

type check struct {
	name   string
	ok     bool
	detail string
}

func newDoctorCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that anitui's dependencies are working",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var checks []check

			if _, err := os.Stat(app.ConfigPath); err == nil {
				checks = append(checks, check{"config", true, app.ConfigPath})
			} else {
				checks = append(checks, check{"config", true, "defaults (no config file)"})
			}

			if s, err := app.Store(cmd.Context()); err != nil {
				checks = append(checks, check{"database", false, err.Error()})
			} else {
				v, _ := s.SchemaVersion(cmd.Context())
				checks = append(checks, check{"database", true, fmt.Sprintf("schema v%d, %s", v, app.Paths.Database())})
			}

			if mpv, err := player.FindMpv(app.Config.Player.MpvPath); err != nil {
				checks = append(checks, check{"mpv", false, err.Error()})
			} else {
				checks = append(checks, check{"mpv", true, mpv})
			}
			if bin, err := browser.FindBinary(app.Config.Browser.Path); err != nil {
				detail := err.Error()
				if app.Config.Browser.AutoDownload {
					detail += " (will be downloaded on first use)"
				}
				checks = append(checks, check{"browser", app.Config.Browser.AutoDownload, detail})
			} else {
				checks = append(checks, check{"browser", true, bin})
			}

			if s, err := app.Store(cmd.Context()); err == nil {
				if list, err := s.Clearances(cmd.Context()); err == nil {
					detail := "none stored"
					if len(list) > 0 {
						var hosts []string
						for _, c := range list {
							hosts = append(hosts, fmt.Sprintf("%s (%s)", c.Host, c.ObtainedAt.Format("2006-01-02")))
						}
						detail = strings.Join(hosts, ", ")
					}
					checks = append(checks, check{"clearances", true, detail})
				}
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			failed := false
			for _, c := range checks {
				mark := "ok"
				if !c.ok {
					mark, failed = "FAIL", true
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", c.name, mark, c.detail)
			}
			w.Flush()
			if failed {
				return errors.New("some checks failed")
			}
			return nil
		},
	}
}
