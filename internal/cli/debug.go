package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/player"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/streamproxy"
)

// newDebugCmd exposes each layer on its own so providers can be checked
// without the UI. Hidden from help.
func newDebugCmd(app *App) *cobra.Command {
	var mode, quality string
	var start time.Duration
	cmd := &cobra.Command{
		Use:    "debug",
		Short:  "Low-level provider commands for troubleshooting",
		Hidden: true,
	}
	cmd.PersistentFlags().StringVar(&mode, "mode", "", "sub or dub (default from config)")
	cmd.PersistentFlags().StringVar(&quality, "quality", "", "best, worst or a height like 720 (default from config)")

	modeOf := func() (domain.Mode, error) {
		m := mode
		if m == "" {
			m = app.Config.General.Mode
		}
		if m != string(domain.Sub) && m != string(domain.Dub) {
			return "", fmt.Errorf("--mode must be sub or dub, not %q", m)
		}
		return domain.Mode(m), nil
	}
	providerOf := func(ctx context.Context, name string) (provider.Provider, error) {
		reg, err := app.Providers(ctx)
		if err != nil {
			return nil, err
		}
		return reg.Get(name)
	}
	// episodeOf fetches the episode list and picks one by canonical number.
	episodeOf := func(ctx context.Context, p provider.Provider, showID, number string, m domain.Mode) (domain.Episode, error) {
		n, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return domain.Episode{}, fmt.Errorf("episode must be a number, not %q", number)
		}
		eps, err := p.Episodes(ctx, showID, m)
		if err != nil {
			return domain.Episode{}, err
		}
		return provider.FindEpisode(eps, n)
	}

	playCmd := &cobra.Command{
		Use:   "play <provider> <show-id> <episode>",
		Short: "Resolve an episode and play it in mpv",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			m, err := modeOf()
			if err != nil {
				return err
			}
			p, err := providerOf(ctx, args[0])
			if err != nil {
				return err
			}
			ep, err := episodeOf(ctx, p, args[1], args[2], m)
			if err != nil {
				return err
			}
			streams, err := p.Streams(ctx, args[1], ep, m)
			if err != nil {
				return err
			}
			q := quality
			if q == "" {
				q = app.Config.General.Quality
			}
			stream, err := domain.SelectStream(streams, q)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Playing episode %s: %s\n", ep.Label(), stream.Label)
			return playDebug(ctx, app, stream, fmt.Sprintf("Episode %s", ep.Label()), start)
		},
	}
	playCmd.Flags().DurationVar(&start, "start", 0, "start position, e.g. 12m30s")

	cmd.AddCommand(
		&cobra.Command{
			Use:   "search <provider> <query>...",
			Short: "Search one provider",
			Args:  cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				m, err := modeOf()
				if err != nil {
					return err
				}
				p, err := providerOf(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				shows, err := p.Search(cmd.Context(), strings.Join(args[1:], " "), m)
				if err != nil {
					return err
				}
				return printJSON(shows)
			},
		},
		&cobra.Command{
			Use:   "episodes <provider> <show-id>",
			Short: "List a show's episodes",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				m, err := modeOf()
				if err != nil {
					return err
				}
				p, err := providerOf(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				eps, err := p.Episodes(cmd.Context(), args[1], m)
				if err != nil {
					return err
				}
				return printJSON(eps)
			},
		},
		&cobra.Command{
			Use:   "ids <provider> <show-id>",
			Short: "Show a show's AniList/MAL IDs",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				p, err := providerOf(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				r, ok := p.(provider.IDResolver)
				if !ok {
					return fmt.Errorf("%s does not resolve IDs separately (search results include them)", p.Name())
				}
				al, mal, err := r.ExternalIDs(cmd.Context(), args[1])
				if err != nil {
					return err
				}
				return printJSON(map[string]int{"anilist_id": al, "mal_id": mal})
			},
		},
		&cobra.Command{
			Use:   "streams <provider> <show-id> <episode>",
			Short: "Resolve streams for an episode",
			Args:  cobra.ExactArgs(3),
			RunE: func(cmd *cobra.Command, args []string) error {
				m, err := modeOf()
				if err != nil {
					return err
				}
				p, err := providerOf(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				ep, err := episodeOf(cmd.Context(), p, args[1], args[2], m)
				if err != nil {
					return err
				}
				streams, err := p.Streams(cmd.Context(), args[1], ep, m)
				if err != nil {
					return err
				}
				return printJSON(streams)
			},
		},
		playCmd,
	)
	return cmd
}

// playDebug plays a stream through the player layer and prints progress. The
// session orchestrator (history, autoplay, skipping) builds on this in Phase 3.
func playDebug(ctx context.Context, app *App, s domain.Stream, title string, start time.Duration) error {
	req := player.Request{URL: s.URL, Title: title, Start: start, Headers: s.Headers, AudioLang: s.AudioLang}
	subURL := func(u string) string { return u }
	if s.NeedsProxy {
		// No overall timeout: the proxy streams long bodies.
		proxy, err := streamproxy.Start(httpx.New(httpx.Options{Timeout: -1}))
		if err != nil {
			return err
		}
		defer proxy.Close()
		req.URL = proxy.Stream(s)
		req.Headers = nil // the proxy adds them
		subURL = func(u string) string { return proxy.URL(u, s.Headers) }
	}
	for _, sub := range s.Subtitles {
		req.Subtitles = append(req.Subtitles, subURL(sub.URL))
	}

	pb, err := player.New(player.Options{
		MpvPath:   app.Config.Player.MpvPath,
		ExtraArgs: app.Config.Player.ExtraArgs,
	}).Play(ctx, req)
	if err != nil {
		return err
	}
	defer pb.Close()
	go func() {
		<-ctx.Done()
		pb.Close()
	}()

	if err := pb.BindKey(ctx, "F2", "anitui-debug"); err != nil {
		slog.Warn("keybind failed", "err", err)
	}
	fmt.Fprintln(os.Stderr, "Connected to mpv. Press F2 in mpv to test the key binding.")

	var lastPrint time.Time
	for e := range pb.Events() {
		slog.Debug("mpv event", "kind", e.Kind, "pos", e.Position, "dur", e.Duration, "paused", e.Paused, "reason", e.Reason, "args", e.Args)
		switch e.Kind {
		case player.EventPosition:
			if time.Since(lastPrint) >= time.Second {
				lastPrint = time.Now()
				fmt.Fprintf(os.Stderr, "\r%s / %s   ", clock(e.Position), clock(e.Duration))
			}
		case player.EventPause:
			if e.Paused {
				fmt.Fprintf(os.Stderr, "\r%s / %s (paused)", clock(e.Position), clock(e.Duration))
			}
		case player.EventMessage:
			fmt.Fprintf(os.Stderr, "\rkey binding received: %v\n", e.Args)
			pb.ShowText(ctx, "anitui: key binding works", 2*time.Second)
		case player.EventEndFile:
			fmt.Fprintf(os.Stderr, "\nplayback ended: %s\n", e.Reason)
		}
	}
	waitErr := pb.Wait()
	st := pb.State()
	if st.Duration > 0 {
		fmt.Fprintf(os.Stderr, "Stopped at %s of %s (%.0f%%)\n", clock(st.Position), clock(st.Duration), 100*st.Position.Seconds()/st.Duration.Seconds())
	}
	if ctx.Err() != nil {
		return nil // interrupted by the user
	}
	return waitErr
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, sec := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
