// Package config defines anitui's user configuration: its schema, defaults,
// loading from TOML, and validation.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Template is the commented config file written by `anitui config init`.
// It must decode to exactly Default(); a test enforces this.
//
//go:embed default.toml
var Template []byte

type Config struct {
	General   General   `toml:"general"`
	Providers Providers `toml:"providers"`
	Player    Player    `toml:"player"`
	Browser   Browser   `toml:"browser"`
	Tracking  Tracking  `toml:"tracking"`
	Skip      Skip      `toml:"skip"`
	Discord   Discord   `toml:"discord"`
	UI        UI        `toml:"ui"`
}

type General struct {
	Mode                string  `toml:"mode"`
	Quality             string  `toml:"quality"`
	AutoplayNext        bool    `toml:"autoplay_next"`
	WatchedThreshold    float64 `toml:"watched_threshold"`
	ResumeRewindSeconds int     `toml:"resume_rewind_seconds"`
}

type Providers struct {
	Order              []string `toml:"order"`
	HealthCheckTimeout Duration `toml:"health_check_timeout"`
}

type Player struct {
	MpvPath   string   `toml:"mpv_path"`
	ExtraArgs []string `toml:"extra_args"`
}

type Browser struct {
	Path         string `toml:"path"`
	Headless     bool   `toml:"headless"`
	AutoDownload bool   `toml:"auto_download"`
}

type Tracking struct {
	Backend         string `toml:"backend"`
	AnilistClientID int    `toml:"anilist_client_id"`
}

type Skip struct {
	Opening        string `toml:"opening"`
	Ending         string `toml:"ending"`
	Recap          string `toml:"recap"`
	FillerEpisodes bool   `toml:"filler_episodes"`
	RecapEpisodes  bool   `toml:"recap_episodes"`
}

type Discord struct {
	Enabled   bool   `toml:"enabled"`
	ClientID  string `toml:"client_id"`
	ShowCover bool   `toml:"show_cover"`
}

type UI struct {
	Theme string `toml:"theme"`
}

// Duration is a time.Duration that reads and writes as a string like "5s".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Known enum values, exported so the TUI settings view can offer them.
var (
	Modes         = []string{"sub", "dub"}
	Qualities     = []string{"best", "1080", "720", "480", "360", "worst"}
	ProviderNames = []string{"anikoto", "senshi", "allanime", "animepahe"}
	Backends      = []string{"local", "anilist"}
	SkipActions   = []string{"auto", "prompt", "off"}
	Themes        = []string{"default", "mono"}
)

func Default() Config {
	return Config{
		General: General{
			Mode:                "sub",
			Quality:             "best",
			AutoplayNext:        true,
			WatchedThreshold:    0.85,
			ResumeRewindSeconds: 5,
		},
		Providers: Providers{
			Order:              []string{"anikoto", "senshi", "allanime", "animepahe"},
			HealthCheckTimeout: Duration{5 * time.Second},
		},
		Player:   Player{ExtraArgs: []string{}},
		Browser:  Browser{Headless: true},
		Tracking: Tracking{Backend: "anilist"},
		Skip: Skip{
			Opening: "auto",
			Ending:  "auto",
			Recap:   "prompt",
		},
		Discord: Discord{Enabled: true, ShowCover: true},
		UI:      UI{Theme: "default"},
	}
}

// Load reads the config at path on top of Default(). A missing file is not an
// error: defaults are returned and exists is false.
func Load(path string) (cfg Config, exists bool, err error) {
	cfg = Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := Decode(data, &cfg); err != nil {
		return cfg, true, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, true, nil
}

// Decode strictly decodes TOML into cfg (unknown keys are errors) and validates
// the result. Decode errors include the offending line with a pointer.
func Decode(data []byte, cfg *Config) error {
	dec := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		var derr *toml.DecodeError
		var serr *toml.StrictMissingError
		// StrictMissingError unwraps to DecodeErrors, so it must be checked first.
		switch {
		case errors.As(err, &serr):
			return fmt.Errorf("unknown config keys:\n%s", serr.String())
		case errors.As(err, &derr):
			row, col := derr.Position()
			return fmt.Errorf("line %d, column %d: %s\n%s", row, col, derr.Error(), derr.String())
		}
		return err
	}
	return cfg.Validate()
}

// Validate reports every invalid value at once.
func (c *Config) Validate() error {
	var errs []error
	oneOf := func(key, val string, allowed []string) {
		if !slices.Contains(allowed, val) {
			errs = append(errs, fmt.Errorf("%s: %q is not one of %s", key, val, strings.Join(allowed, ", ")))
		}
	}

	oneOf("general.mode", c.General.Mode, Modes)
	oneOf("general.quality", c.General.Quality, Qualities)
	if t := c.General.WatchedThreshold; t <= 0 || t > 1 {
		errs = append(errs, fmt.Errorf("general.watched_threshold: %v must be in (0, 1]", t))
	}
	if c.General.ResumeRewindSeconds < 0 {
		errs = append(errs, errors.New("general.resume_rewind_seconds: must not be negative"))
	}

	if len(c.Providers.Order) == 0 {
		errs = append(errs, errors.New("providers.order: at least one provider is required"))
	}
	seen := map[string]bool{}
	for _, p := range c.Providers.Order {
		oneOf("providers.order", p, ProviderNames)
		if seen[p] {
			errs = append(errs, fmt.Errorf("providers.order: %q listed twice", p))
		}
		seen[p] = true
	}
	if c.Providers.HealthCheckTimeout.Duration <= 0 {
		errs = append(errs, errors.New("providers.health_check_timeout: must be positive"))
	}

	oneOf("tracking.backend", c.Tracking.Backend, Backends)
	oneOf("skip.opening", c.Skip.Opening, SkipActions)
	oneOf("skip.ending", c.Skip.Ending, SkipActions)
	oneOf("skip.recap", c.Skip.Recap, SkipActions)
	oneOf("ui.theme", c.UI.Theme, Themes)

	if id := c.Discord.ClientID; id != "" && strings.Trim(id, "0123456789") != "" {
		errs = append(errs, fmt.Errorf("discord.client_id: %q must be a numeric Discord application ID", id))
	}

	return errors.Join(errs...)
}
