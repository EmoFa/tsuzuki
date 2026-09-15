package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/streamproxy"
)

// newDebugCmd exposes each layer on its own so providers can be checked
// without the UI. Hidden from help.
func newDebugCmd(app *App) *cobra.Command {
	var mode, quality string
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
		&cobra.Command{
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
				return playDebug(ctx, app, stream, fmt.Sprintf("Episode %s", ep.Label()))
			},
		},
	)
	return cmd
}

// playDebug runs mpv in the foreground. The real player layer (IPC, progress,
// skipping) replaces this in Phase 2.
func playDebug(ctx context.Context, app *App, s domain.Stream, title string) error {
	target := s.URL
	args := []string{"--force-media-title=" + title}
	subURL := func(u string) string { return u }

	if s.NeedsProxy {
		// No overall timeout: the proxy streams long bodies.
		proxy, err := streamproxy.Start(httpx.New(httpx.Options{Timeout: -1}))
		if err != nil {
			return err
		}
		defer proxy.Close()
		target = proxy.Stream(s)
		subURL = func(u string) string { return proxy.URL(u, s.Headers) }
	} else if len(s.Headers) > 0 {
		keys := make([]string, 0, len(s.Headers))
		for k := range s.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fields := make([]string, 0, len(keys))
		for _, k := range keys {
			fields = append(fields, k+": "+s.Headers[k])
		}
		args = append(args, "--http-header-fields="+strings.Join(fields, ","))
	}
	for _, sub := range s.Subtitles {
		args = append(args, "--sub-file="+subURL(sub.URL))
	}
	if s.AudioLang != "" {
		args = append(args, "--alang="+s.AudioLang)
	}

	mpv := app.Config.Player.MpvPath
	if mpv == "" {
		mpv = "mpv"
	}
	args = append(args, app.Config.Player.ExtraArgs...)
	args = append(args, target)
	c := exec.CommandContext(ctx, mpv, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
