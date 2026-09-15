package config

import (
	"os"
	"path/filepath"
	"reflect"
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
	body := "[general]\nmode = \"dub\"\n[providers]\norder = [\"allanime\"]\nhealth_check_timeout = \"2s\"\n"
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
			"[general]\nmode = \"raw\"\nwatched_threshold = 1.5\n[providers]\norder = [\"allanime\", \"allanime\", \"nyaa\"]\n[discord]\nclient_id = \"anitui\"\n",
			[]string{"general.mode", "watched_threshold", "listed twice", `"nyaa"`, "discord.client_id"},
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
	t.Setenv("ANITUI_CONFIG_DIR", "/tmp/a")
	t.Setenv("ANITUI_DATA_DIR", "/tmp/b")
	t.Setenv("ANITUI_CACHE_DIR", "/tmp/c")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile() != filepath.Join("/tmp/a", "config.toml") || p.Database() != filepath.Join("/tmp/b", "anitui.db") || p.LogFile() != filepath.Join("/tmp/c", "anitui.log") {
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
