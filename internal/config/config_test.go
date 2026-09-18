package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestTemplateMatchesDefault(t *testing.T) {
	var cfg Config
	if err := Decode(Template, &cfg); err != nil {
		t.Fatalf("template does not decode: %v", err)
	}
	if want := Default(); !reflect.DeepEqual(cfg, want) {
		t.Fatalf("template drifted from Default():\n got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, exists, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatal("expected defaults")
	}
}

func TestLoadPartialOverridesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[general]\nmode = \"dub\"\n[providers]\norder = [\"senshi\"]\nhealth_check_timeout = \"2s\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, exists, err := Load(path)
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	if cfg.General.Mode != "dub" || cfg.General.Quality != "best" {
		t.Errorf("general = %+v", cfg.General)
	}
	if len(cfg.Providers.Order) != 1 || cfg.Providers.HealthCheckTimeout.Duration != 2*time.Second {
		t.Errorf("providers = %+v", cfg.Providers)
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name, body string
		want       []string
	}{
		{"syntax", "[general]\nmode = \n", []string{"line 2"}},
		{"unknown key", "[general]\nmood = \"sub\"\n", []string{"unknown config keys", "mood"}},
		{"bad duration", "[providers]\nhealth_check_timeout = \"soon\"\n", []string{"line 2"}},
		{
			"invalid values reported together",
			"[general]\nmode = \"raw\"\nwatched_threshold = 1.5\n[providers]\norder = [\"senshi\", \"senshi\", \"nyaa\"]\n[discord]\nclient_id = \"tsuzuki\"\n[subtitles]\nlanguages = [\"English\"]\n",
			[]string{"general.mode", "watched_threshold", "listed twice", `"nyaa"`, "discord.client_id", "subtitles.languages"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			err := Decode([]byte(tt.body), &cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
		})
	}
}

func TestResolvePathsEnvOverride(t *testing.T) {
	t.Setenv("TSUZUKI_CONFIG_DIR", "/tmp/a")
	t.Setenv("TSUZUKI_DATA_DIR", "/tmp/b")
	t.Setenv("TSUZUKI_CACHE_DIR", "/tmp/c")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile() != filepath.Join("/tmp/a", "config.toml") || p.Database() != filepath.Join("/tmp/b", "tsuzuki.db") || p.LogFile() != filepath.Join("/tmp/c", "tsuzuki.log") {
		t.Fatalf("%+v", p)
	}
}

// TestDocs checks that docs/config.md describes every key in the default config.
func TestDocs(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "config.md"))
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]map[string]any
	if err := toml.Unmarshal(Template, &tree); err != nil {
		t.Fatal(err)
	}
	for section, keys := range tree {
		for key := range keys {
			if name := "`" + section + "." + key + "`"; !strings.Contains(string(doc), "| "+name+" |") {
				t.Errorf("docs/config.md has no table row for %s", name)
			}
		}
	}
}

func TestWindowsPathHint(t *testing.T) {
	var cfg Config
	err := Decode([]byte("[player]\nmpv_path = \"C:\\Program Files\\mpv\\mpv.exe\"\n"), &cfg)
	if err == nil || !strings.Contains(err.Error(), "single quotes") {
		t.Fatalf("err = %v", err)
	}
	cfg = Default()
	if err := Decode([]byte("[player]\nmpv_path = 'C:\\Program Files\\mpv\\mpv.exe'\n"), &cfg); err != nil || cfg.Player.MpvPath != `C:\Program Files\mpv\mpv.exe` {
		t.Fatalf("single quotes: %q %v", cfg.Player.MpvPath, err)
	}
}

func TestRemovedProvidersStayValidButAreSkipped(t *testing.T) {
	cfg := Default()
	body := "[providers]\norder = [\"allanime\", \"senshi\"]\n"
	if err := Decode([]byte(body), &cfg); err != nil {
		t.Fatalf("a config naming a removed provider should still load: %v", err)
	}
	if got := cfg.ActiveProviders(); !slices.Equal(got, []string{"senshi"}) {
		t.Errorf("ActiveProviders = %v", got)
	}
	if got := cfg.DroppedProviders(); !slices.Equal(got, []string{"allanime"}) {
		t.Errorf("DroppedProviders = %v", got)
	}
	// Unknown providers are still errors.
	cfg = Default()
	if err := Decode([]byte("[providers]\norder = [\"nyaa\"]\n"), &cfg); err == nil {
		t.Error("unknown provider accepted")
	}
}
