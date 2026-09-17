package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
)

func newSearchCmd(app *App) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>...",
		Short: "Search AniList for anime",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			al, err := app.AniList(cmd.Context())
			if err != nil {
				return err
			}
			results, err := al.Search(cmd.Context(), strings.Join(args, " "), limit)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No results.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tFORMAT\tEPISODES\tYEAR\tTITLE")
			for _, m := range results {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", m.ID, m.Format, episodesLabel(m), yearLabel(m.Year), m.DisplayTitle())
			}
			w.Flush()
			fmt.Fprintln(cmd.OutOrStdout(), "\nWatch with: tsuzuki watch <id> [episode]")
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "number of results")
	return cmd
}

func newWatchCmd(app *App) *cobra.Command {
	var mode, prefer, subs string
	cmd := &cobra.Command{
		Use:   "watch <anilist-id> [episode]",
		Short: "Watch an anime, resuming where you left off",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("anilist id must be a number, not %q", args[0])
			}
			var episode float64
			if len(args) == 2 {
				if episode, err = strconv.ParseFloat(args[1], 64); err != nil || episode <= 0 {
					return fmt.Errorf("episode must be a positive number, not %q", args[1])
				}
			}
			m, err := modeFlag(app, mode, "")
			if err != nil {
				return err
			}
			subPrefs, err := subsFlag(app, subs)
			if err != nil {
				return err
			}
			return watch(cmd.Context(), app, id, episode, m, prefer, subPrefs)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "", "sub or dub (default from config)")
	cmd.Flags().StringVar(&prefer, "provider", "", "try this provider first")
	cmd.Flags().StringVar(&subs, "subs", "", `subtitle languages for this watch, e.g. "es,en", or "off" to start hidden`)
	return cmd
}

func newContinueCmd(app *App) *cobra.Command {
	var mode, subs string
	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Continue the anime you watched most recently",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := app.Store(ctx)
			if err != nil {
				return err
			}
			recent, err := st.RecentShows(ctx, 1)
			if err != nil {
				return err
			}
			if len(recent) == 0 {
				return errors.New("nothing watched yet; find something with `tsuzuki search`")
			}
			last := recent[0]
			// Keep watching in the mode used last time unless told otherwise.
			m, err := modeFlag(app, mode, last.Mode)
			if err != nil {
				return err
			}
			subPrefs, err := subsFlag(app, subs)
			if err != nil {
				return err
			}
			return watch(ctx, app, last.MediaID, 0, m, last.Provider, subPrefs)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "", "sub or dub (default: the mode you used last)")
	cmd.Flags().StringVar(&subs, "subs", "", `subtitle languages for this watch, e.g. "es,en", or "off" to start hidden`)
	return cmd
}

func newHistoryCmd(app *App) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List recently watched anime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := app.Store(ctx)
			if err != nil {
				return err
			}
			recent, err := st.RecentShows(ctx, limit)
			if err != nil {
				return err
			}
			if len(recent) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing watched yet.")
				return nil
			}
			al, err := app.AniList(ctx)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tLAST EPISODE\tWHEN")
			for _, p := range recent {
				title := fmt.Sprintf("(AniList #%d)", p.MediaID)
				if m, err := al.Media(ctx, p.MediaID); err == nil {
					title = m.DisplayTitle()
				}
				state := "watched"
				if !p.Completed {
					state = clock(p.Position) + " / " + clock(p.Duration)
				}
				fmt.Fprintf(w, "%d\t%s\t%s (%s)\t%s\n", p.MediaID, title, strconv.FormatFloat(p.Episode, 'f', -1, 64), state, ago(p.UpdatedAt))
			}
			return w.Flush()
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "number of shows")
	return cmd
}

// watch looks up the media and runs a session with terminal status output.
func watch(ctx context.Context, app *App, mediaID int, episode float64, mode domain.Mode, prefer string, subs *domain.SubtitlePrefs) error {
	al, err := app.AniList(ctx)
	if err != nil {
		return err
	}
	media, err := al.Media(ctx, mediaID)
	if err != nil {
		return err
	}
	if episode == 0 {
		// Report "caught up" instead of failing to find an unaired episode.
		st, err := app.Store(ctx)
		if err != nil {
			return err
		}
		next, err := session.NextToWatch(ctx, st, mediaID)
		if err != nil {
			return err
		}
		if aired := media.AiredEpisodes(); aired > 0 && next > float64(aired) {
			fmt.Fprintf(os.Stderr, "You're caught up on %s (%d episodes).\n", media.DisplayTitle(), aired)
			return nil
		}
		episode = next
	}

	// Send list changes left over from earlier runs before starting.
	if t, err := app.Tracker(ctx); err == nil && t.Remote != nil {
		flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if _, err := t.Flush(flushCtx); err != nil {
			slog.Warn("sending queued list changes", "err", err)
		}
		cancel()
	}

	printer := &statusPrinter{w: os.Stderr}
	sess, err := app.Session(ctx, printer.print)
	if err != nil {
		return err
	}
	err = sess.Watch(ctx, session.Request{Media: media, Episode: episode, Mode: mode, Provider: prefer, Subtitles: subs})
	printer.endLine()
	if ctx.Err() != nil {
		return nil // interrupted
	}
	return err
}

// subsFlag reads --subs: "off" hides subtitles, a comma-separated list sets the
// languages (and shows them). Empty uses the show's or config's settings.
func subsFlag(app *App, flag string) (*domain.SubtitlePrefs, error) {
	switch flag = strings.TrimSpace(strings.ToLower(flag)); flag {
	case "":
		return nil, nil
	case "off", "none", "hide":
		p := app.subtitlePrefs()
		p.Show = false
		return &p, nil
	}
	p := domain.SubtitlePrefs{Show: true}
	for _, lang := range strings.Split(flag, ",") {
		lang = strings.TrimSpace(lang)
		if !config.IsLanguageCode(lang) {
			return nil, fmt.Errorf("--subs: %q is not a language code like \"en\" (or use \"off\")", lang)
		}
		p.Languages = append(p.Languages, lang)
	}
	return &p, nil
}

func modeFlag(app *App, flag, fallback string) (domain.Mode, error) {
	m := flag
	if m == "" {
		m = fallback
	}
	if m == "" {
		m = app.Config.General.Mode
	}
	if m != string(domain.Sub) && m != string(domain.Dub) {
		return "", fmt.Errorf("--mode must be sub or dub, not %q", m)
	}
	return domain.Mode(m), nil
}

// statusPrinter renders session updates, keeping the progress on one line.
type statusPrinter struct {
	w            io.Writer
	onProgress   bool
	lastProgress time.Time
}

func (p *statusPrinter) print(s session.Status) {
	ep := strconv.FormatFloat(s.Episode, 'f', -1, 64)
	switch s.Kind {
	case session.StatusResolving:
		p.line("Looking for episode %s on %s…", ep, s.Provider)
	case session.StatusProviderFailed:
		p.line("  ✗ %s", firstLineOf(s.Err.Error())) // errors already name the provider
	case session.StatusPlaying:
		msg := fmt.Sprintf("▶ %s · Episode %s · %s %s", s.Media.DisplayTitle(), ep, s.Provider, s.Stream.Label)
		if s.Start > 0 {
			msg += " · resuming at " + clock(s.Start)
		}
		p.line("%s", msg)
	case session.StatusProgress:
		if s.Duration == 0 || time.Since(p.lastProgress) < time.Second {
			return
		}
		p.lastProgress = time.Now()
		fmt.Fprintf(p.w, "\r  %s / %s ", clock(s.Position), clock(s.Duration))
		p.onProgress = true
	case session.StatusWatched:
		p.line("✓ Episode %s marked as watched", ep)
	case session.StatusEpisodeSkipped:
		p.line("⏭ Skipping %s (%s)", session.EpisodeRange(s.Episode, s.Through), s.Reason)
	case session.StatusSkipped:
		p.line("  ⏭ Skipped %s", s.Reason)
	case session.StatusTracked:
		if s.Err != nil {
			p.line("  ✗ updating your list: %s", firstLineOf(s.Err.Error()))
		} else {
			p.line("  %s", s.Reason)
		}
	case session.StatusStopped:
		if s.Reason != "eof" && s.Duration > 0 {
			p.line("Stopped episode %s at %s / %s", ep, clock(s.Position), clock(s.Duration))
		}
	case session.StatusNoNextEpisode:
		p.line("No episode after %s is available yet.", ep)
	}
}

func (p *statusPrinter) line(format string, args ...any) {
	p.endLine()
	fmt.Fprintf(p.w, format+"\n", args...)
}

func (p *statusPrinter) endLine() {
	if p.onProgress {
		fmt.Fprintln(p.w)
		p.onProgress = false
	}
}

func episodesLabel(m anilist.Media) string {
	switch {
	case m.NextAiringEpisode != nil && m.Episodes > 0:
		return fmt.Sprintf("%d/%d", m.AiredEpisodes(), m.Episodes)
	case m.NextAiringEpisode != nil:
		return fmt.Sprintf("%d+", m.AiredEpisodes())
	case m.Episodes > 0:
		return strconv.Itoa(m.Episodes)
	}
	return "?"
}

func yearLabel(y int) string {
	if y == 0 {
		return "-"
	}
	return strconv.Itoa(y)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
