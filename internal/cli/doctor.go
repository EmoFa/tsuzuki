package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"text/tabwriter"

	"github.com/spf13/cobra"
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

			checks = append(checks, binaryCheck("mpv", app.Config.Player.MpvPath, "mpv"))
			checks = append(checks, binaryCheck("browser", app.Config.Browser.Path,
				"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome", "msedge"))

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

// binaryCheck looks up a configured path, or the first candidate found on PATH.
// This is a presence check only; the player and browser packages will replace
// it with real detection.
func binaryCheck(name, configured string, candidates ...string) check {
	if configured != "" {
		candidates = []string{configured}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return check{name, true, p}
		}
	}
	return check{name, false, "not found (tried " + fmt.Sprint(candidates) + ")"}
}
