package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/auth"
	"github.com/EmoFa/anitui/internal/tracker"
)

func newLoginCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to AniList so watched episodes sync to your list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := login(cmd.Context(), app, func(url string) {
				fmt.Fprintf(os.Stderr, "Opening AniList in your browser to approve anitui…\nIf nothing opens, visit:\n  %s\n", url)
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
				fmt.Fprintln(out, "Not logged in to AniList. Run `anitui login`.")
			case !tok.Valid():
				fmt.Fprintf(out, "AniList login for %s has expired. Run `anitui login`.\n", tok.UserName)
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
		return "", errors.New("not logged in to AniList; run `anitui login`")
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
