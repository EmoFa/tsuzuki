package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/auth"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

func newLoginCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to AniList so watched episodes sync to your list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := login(cmd.Context(), app, func(url string) {
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
			if msg, err := syncList(cmd.Context(), app); err != nil {
				fmt.Fprintln(os.Stderr, "Couldn't fetch your list yet:", err)
			} else {
				fmt.Fprintln(os.Stderr, msg)
			}
			return nil
		},
	}
}

// login runs the browser flow, verifies the token and stores it.
func login(ctx context.Context, app *App, show func(url string)) (string, error) {
	clientID := app.aniListClientID()
	grant, err := auth.Login(ctx, auth.LoginOptions{
		ClientID: clientID,
		Open: func(url string) error {
			show(url)
			return auth.OpenBrowser(url)
		},
	})
	if errors.Is(err, auth.ErrNoClientID) {
		return "", fmt.Errorf(`%w. Create one at https://anilist.co/settings/developer with redirect URL
%s, then set tracking.anilist_client_id in %s`, err, auth.RedirectURL, app.ConfigPath)
	}
	if err != nil {
		return "", err
	}
	al, err := app.AniList(ctx)
	if err != nil {
		return "", err
	}
	user, err := al.WithToken(grant.AccessToken).Viewer(ctx)
	if err != nil {
		return "", fmt.Errorf("checking the new login: %w", err)
	}
	tok := auth.Token{AccessToken: grant.AccessToken, UserID: user.ID, UserName: user.Name}
	if grant.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(grant.ExpiresIn)
	}
	if err := app.tokenFile().Save(tok); err != nil {
		return "", err
	}
	app.lazyMu.Lock()
	app.tracker = nil // rebuild with the new login
	app.lazyMu.Unlock()
	return user.Name, nil
}

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the AniList login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.tokenFile().Delete(); err != nil {
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
			tok, err := app.tokenFile().Load()
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
			msg, err := syncList(cmd.Context(), app)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, msg)
			return nil
		},
	}
}

// syncList flushes queued changes and pulls the remote list.
func syncList(ctx context.Context, app *App) (string, error) {
	t, err := app.Tracker(ctx)
	if err != nil {
		return "", err
	}
	if t.Remote == nil {
		if app.Config.Tracking.Backend != "anilist" {
			return "", errors.New(`tracking.backend is "local"; nothing to sync`)
		}
		return "", errors.New("not logged in to AniList; run `tsuzuki login`")
	}
	sent, err := t.Flush(ctx)
	if err != nil {
		return "", fmt.Errorf("sending list changes: %w", err)
	}
	pulled, err := t.Pull(ctx)
	if err != nil {
		return "", fmt.Errorf("fetching your list: %w", err)
	}
	return fmt.Sprintf("Synced with AniList: sent %d change(s), fetched %d entries.", sent, pulled), nil
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
				fmt.Fprintf(w, "%d\t%s\t%d/%s\t%s\t%s\n", e.MediaID, StatusLabel(e.Status), e.Progress, total, score, title)
			}
			return w.Flush()
		},
	}
}

// StatusLabel is the friendly name for a list status.
func StatusLabel(s string) string {
	switch s {
	case tracker.Current:
		return "watching"
	case tracker.Repeating:
		return "rewatching"
	}
	return strings.ToLower(s)
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
			fmt.Fprintln(cmd.OutOrStdout(), scoreNote(r))
			return nil
		},
	}
}

// scoreNote describes a SetScore result.
func scoreNote(r tracker.Result) string {
	score := strconv.FormatFloat(r.Entry.Score, 'f', -1, 64)
	switch {
	case !r.Changed:
		return "Already rated " + score + "/10."
	case r.Synced:
		return "Rated " + score + "/10 on AniList."
	case errors.Is(r.SyncErr, anilist.ErrUnauthorized):
		return "Rated " + score + "/10; AniList login expired, run `tsuzuki login` to sync."
	case r.SyncErr != nil:
		return "Rated " + score + "/10; AniList sync pending: " + firstLineOf(r.SyncErr.Error())
	}
	return "Rated " + score + "/10."
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
			st, err := app.Store(ctx)
			if err != nil {
				return err
			}
			round, err := st.StartRound(ctx, id)
			if err != nil {
				return err
			}
			t, err := app.Tracker(ctx)
			if err != nil {
				return err
			}
			r, err := t.StartRewatch(ctx, id)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Rewatch started: round %d. `tsuzuki continue` plays episode 1.\n", round)
			switch {
			case r.Synced:
				fmt.Fprintln(out, "AniList set to rewatching.")
			case errors.Is(r.SyncErr, anilist.ErrUnauthorized):
				fmt.Fprintln(out, "AniList login expired; run `tsuzuki login` to sync.")
			case r.SyncErr != nil:
				fmt.Fprintln(out, "AniList sync pending: "+firstLineOf(r.SyncErr.Error()))
			}
			return nil
		},
	}
}
