package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/core"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

func newLoginCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to AniList so watched episodes sync to your list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := app.Login(cmd.Context(), func(url string) {
				fmt.Fprintf(os.Stderr, "Opening AniList in your browser to approve tsuzuki…\nIf nothing opens, visit:\n  %s\n", url)
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Logged in as %s.\n", name)
			if app.Config.Tracking.Backend != "anilist" {
				fmt.Fprintln(os.Stderr, `tracking.backend is "local", so nothing will sync. Set it to "anilist" to sync your list.`)
				return nil
			}
			if msg, err := app.Sync(cmd.Context()); err != nil {
				fmt.Fprintln(os.Stderr, "Couldn't fetch your list yet:", err)
			} else {
				fmt.Fprintln(os.Stderr, msg)
			}
			return nil
		},
	}
}

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the AniList login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.Logout(); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "Logged out. Your local list is kept.")
			return nil
		},
	}
}

func newWhoamiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the AniList login and sync status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			tok, err := app.TokenFile().Load()
			if err != nil {
				return err
			}
			switch {
			case tok == nil:
				fmt.Fprintln(out, "Not logged in to AniList. Run `tsuzuki login`.")
			case !tok.Valid():
				fmt.Fprintf(out, "AniList login for %s has expired. Run `tsuzuki login`.\n", tok.UserName)
			default:
				fmt.Fprintf(out, "Logged in to AniList as %s", tok.UserName)
				if !tok.ExpiresAt.IsZero() {
					fmt.Fprintf(out, " (until %s)", tok.ExpiresAt.Format("2006-01-02"))
				}
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "Tracking backend: %s\n", app.Config.Tracking.Backend)
			if st, err := app.Store(cmd.Context()); err == nil {
				if pending, err := st.PendingSyncs(cmd.Context()); err == nil && len(pending) > 0 {
					fmt.Fprintf(out, "%d list change(s) waiting to sync; last error: %s\n", len(pending), pending[len(pending)-1].LastError)
				}
			}
			return nil
		},
	}
}

func newSyncCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Send pending list changes to AniList and fetch your list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			msg, err := app.Sync(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, msg)
			return nil
		},
	}
}

func newListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list [status]",
		Short: "Show your anime list (watching, planning, completed, paused, dropped, repeating)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			status := ""
			if len(args) == 1 {
				var ok bool
				if status, ok = statusFromWord(args[0]); !ok {
					return fmt.Errorf("unknown status %q", args[0])
				}
			}
			st, err := app.Store(ctx)
			if err != nil {
				return err
			}
			entries, err := st.ListEntries(ctx, status)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Your list is empty.")
				return nil
			}
			al, err := app.AniList(ctx)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATUS\tPROGRESS\tSCORE\tTITLE")
			for _, e := range entries {
				title, total := fmt.Sprintf("(AniList #%d)", e.MediaID), "?"
				if m, err := al.Media(ctx, e.MediaID); err == nil {
					title = m.DisplayTitle()
					if m.Episodes > 0 {
						total = fmt.Sprint(m.Episodes)
					}
				}
				score := "-"
				if e.Score > 0 {
					score = fmt.Sprintf("%.1f", e.Score)
				}
				fmt.Fprintf(w, "%d\t%s\t%d/%s\t%s\t%s\n", e.MediaID, core.StatusLabel(e.Status), e.Progress, total, score, title)
			}
			return w.Flush()
		},
	}
}

func statusFromWord(w string) (string, bool) {
	switch strings.ToLower(w) {
	case "watching", "current":
		return tracker.Current, true
	case "planning", "plan":
		return tracker.Planning, true
	case "completed", "done":
		return tracker.Completed, true
	case "paused", "on-hold":
		return tracker.Paused, true
	case "dropped":
		return tracker.Dropped, true
	case "repeating", "rewatching":
		return tracker.Repeating, true
	}
	return "", false
}

func newRateCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rate <anilist-id> <score>",
		Short: "Score a show on your list from 1 to 10 (synced to AniList when logged in)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("anilist id must be a number, not %q", args[0])
			}
			score, err := strconv.ParseFloat(args[1], 64)
			if err != nil || score < 1 || score > 10 {
				return fmt.Errorf("score must be a number from 1 to 10, not %q", args[1])
			}
			t, err := app.Tracker(ctx)
			if err != nil {
				return err
			}
			r, err := t.SetScore(ctx, id, score)
			if errors.Is(err, tracker.ErrNotOnList) {
				return fmt.Errorf("AniList #%d isn't on your list yet; watch it or add it from its details screen first", id)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), core.ScoreNote(r))
			return nil
		},
	}
}

func newRewatchCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rewatch <anilist-id>",
		Short: "Start watching a show again from episode 1, keeping your history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("anilist id must be a number, not %q", args[0])
			}
			rw, err := app.StartRewatch(ctx, id)
			if err != nil {
				return err
			}
			r := rw.Result
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Rewatch started: round %d. `tsuzuki continue` plays episode 1.\n", rw.Round)
			switch {
			case r.Synced:
				fmt.Fprintln(out, "AniList set to rewatching.")
			case errors.Is(r.SyncErr, anilist.ErrUnauthorized):
				fmt.Fprintln(out, "AniList login expired; run `tsuzuki login` to sync.")
			case r.SyncErr != nil:
				fmt.Fprintln(out, "AniList sync pending: "+core.FirstLine(r.SyncErr.Error()))
			}
			return nil
		},
	}
}

func newMarkCmd(app *App) *cobra.Command {
	var unwatched bool
	cmd := &cobra.Command{
		Use:   "mark <anilist-id> <episode|first-last>",
		Short: "Mark episodes as watched (or not), for what you watched elsewhere",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("anilist id must be a number, not %q", args[0])
			}
			from, to, err := episodeRangeArg(args[1])
			if err != nil {
				return err
			}
			al, err := app.AniList(ctx)
			if err != nil {
				return err
			}
			media, err := al.Media(ctx, id)
			if err != nil {
				return err
			}
			note, err := app.SetWatched(ctx, media, from, to, !unwatched)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), note)
			return nil
		},
	}
	cmd.Flags().BoolVar(&unwatched, "unwatched", false, "mark them unwatched instead")
	return cmd
}

// episodeRangeArg reads "7" or "7-12".
func episodeRangeArg(arg string) (from, to float64, err error) {
	first, last, ranged := strings.Cut(arg, "-")
	from, err = strconv.ParseFloat(strings.TrimSpace(first), 64)
	if err != nil || from <= 0 {
		return 0, 0, fmt.Errorf("episode must be a positive number, not %q", first)
	}
	to = from
	if ranged {
		to, err = strconv.ParseFloat(strings.TrimSpace(last), 64)
		if err != nil || to < from {
			return 0, 0, fmt.Errorf("%q is not an episode range like 7-12", arg)
		}
	}
	if to-from > 2000 {
		return 0, 0, fmt.Errorf("%q covers too many episodes", arg)
	}
	return from, to, nil
}
