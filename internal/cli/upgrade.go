package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/update"
)

// upgradeTimeout bounds looking up the latest release.
const upgradeTimeout = 15 * time.Second

func newUpgradeCmd(app *App) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"update"},
		Short:   "Replace this tsuzuki with the latest release",
		Long: "Replace this tsuzuki with the latest release.\n\n" +
			"Only a binary installed by hand is replaced: one from Homebrew, winget, the AUR\n" +
			"or `go install` belongs to that package manager, and the command to use is shown\n" +
			"instead.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{skipConfigAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd.Context(), app, cmd.OutOrStdout(), check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "say what upgrading would do, without doing it")
	return cmd
}

func runUpgrade(ctx context.Context, app *App, out io.Writer, checkOnly bool) error {
	in := installed()
	// Both checks are worth making before the network call: they decide
	// whether upgrading is this command's job at all.
	if !update.IsRelease(buildinfo.Version) {
		return fmt.Errorf("%s is a build from source, not a release: `make build` to update it", buildinfo.Version)
	}
	if !in.SelfUpgrades() {
		return fmt.Errorf("%w: upgrade it with `%s`", update.ErrNotSelfUpgradable, in.Command())
	}

	lookup, cancel := context.WithTimeout(ctx, upgradeTimeout)
	defer cancel()
	rel, err := (&update.Checker{Client: httpx.New(httpx.Options{Retries: -1})}).Latest(lookup)
	if err != nil {
		return err
	}
	if !update.Newer(buildinfo.Version, rel.Version) {
		fmt.Fprintf(out, "tsuzuki %s is the latest release.\n", buildinfo.Version)
		return nil
	}
	if checkOnly {
		fmt.Fprintf(out, "tsuzuki %s is available (you have %s). Run `tsuzuki upgrade` to install it.\n",
			rel.Version, buildinfo.Version)
		return nil
	}

	u := &update.Upgrader{Current: buildinfo.Version, Install: in, Progress: progressLine(out)}
	// Say nothing about downloading until it's clear the binary can be replaced.
	if err := u.Check(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Downloading tsuzuki %s…\n", rel.Version)
	version, err := u.Upgrade(ctx, rel)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Upgraded to %s. Run tsuzuki again to use it.\n", version)
	return nil
}

// progressLine reports a download as a percentage, rewriting one line.
func progressLine(out io.Writer) func(done, total int64) {
	last := -1
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := int(done * 100 / total); pct != last {
			last = pct
			fmt.Fprintf(out, "\r  %d%% of %s", pct, byteSize(total))
			if done >= total {
				fmt.Fprintln(out)
			}
		}
	}
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// upgradeNote runs an upgrade for the UI, which wants one line to show.
func upgradeNote(ctx context.Context) (string, error) {
	rel, err := (&update.Checker{Client: httpx.New(httpx.Options{Retries: -1})}).Latest(ctx)
	if err != nil {
		return "", err
	}
	version, err := (&update.Upgrader{Current: buildinfo.Version, Install: installed()}).Upgrade(ctx, rel)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Upgraded to %s. Restart tsuzuki to use it.", version), nil
}
