// Package update checks GitHub for a newer tsuzuki release and says how to
// upgrade the way it was installed. It never downloads or replaces anything.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/EmoFa/tsuzuki/internal/httpx"
)

// LatestURL is GitHub's latest-release endpoint for tsuzuki.
const LatestURL = "https://api.github.com/repos/EmoFa/tsuzuki/releases/latest"

// ReleasesURL is where releases can be downloaded by hand.
const ReleasesURL = "https://github.com/EmoFa/tsuzuki/releases/latest"

const (
	cacheKey = "update:latest"
	cacheTTL = 24 * time.Hour
)

// Cache stores small values by key (the store's kv table).
type Cache interface {
	GetKV(ctx context.Context, key string) (value string, updated time.Time, ok bool, err error)
	PutKV(ctx context.Context, key, value string) error
}

// Release is a published version.
type Release struct {
	Version string `json:"version"` // without the leading "v"
	URL     string `json:"url"`
}

type Checker struct {
	Client *httpx.Client
	Cache  Cache  // optional
	URL    string // default LatestURL
}

// Latest returns the newest release, checking GitHub at most once a day.
func (c *Checker) Latest(ctx context.Context) (Release, error) {
	if c.Cache != nil {
		if v, updated, ok, err := c.Cache.GetKV(ctx, cacheKey); err == nil && ok && time.Since(updated) < cacheTTL {
			var r Release
			if json.Unmarshal([]byte(v), &r) == nil && r.Version != "" {
				return r, nil
			}
		}
	}
	url := c.URL
	if url == "" {
		url = LatestURL
	}
	var resp struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if err := c.Client.GetJSON(ctx, url, headers, &resp); err != nil {
		return Release{}, fmt.Errorf("checking for updates: %w", err)
	}
	r := Release{Version: strings.TrimPrefix(resp.TagName, "v"), URL: resp.HTMLURL}
	if r.Version == "" {
		return Release{}, fmt.Errorf("checking for updates: no release found")
	}
	if c.Cache != nil {
		data, _ := json.Marshal(r)
		if err := c.Cache.PutKV(ctx, cacheKey, string(data)); err != nil {
			slog.Warn("caching latest release", "err", err)
		}
	}
	return r, nil
}

// Newer reports whether latest is a newer release than current. Development
// builds, snapshots and prereleases never are.
func Newer(current, latest string) bool {
	cur, ok := parse(current)
	if !ok {
		return false
	}
	lat, ok := parse(latest)
	if !ok {
		return false
	}
	for i := range cur {
		if lat[i] != cur[i] {
			return lat[i] > cur[i]
		}
	}
	return false
}

// parse reads "1.2.3" or "v1.2.3"; anything with a suffix ("-rc1", "-next",
// "-dirty") or not three numbers isn't a release.
func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// UpgradeCommand says how to upgrade a tsuzuki installed at exe.
func UpgradeCommand(exe string) string {
	return upgradeCommand(exe, runtime.GOOS, os.Getenv, fileExists)
}

func upgradeCommand(exe, goos string, getenv func(string) string, exists func(string) bool) string {
	slash := strings.ReplaceAll(exe, `\`, "/") // Windows paths, whatever OS runs this
	lower := strings.ToLower(slash)
	switch {
	case strings.Contains(lower, "/caskroom/") || strings.Contains(lower, "/cellar/") ||
		strings.Contains(lower, "/homebrew/") || strings.Contains(lower, "/.linuxbrew/") ||
		(getenv("HOMEBREW_PREFIX") != "" && strings.HasPrefix(slash, filepath.ToSlash(getenv("HOMEBREW_PREFIX"))+"/")):
		return "brew upgrade tsuzuki"
	case goos == "windows" && strings.Contains(lower, "/winget/"):
		return "winget upgrade EmoFa.tsuzuki"
	case isGoBin(slash, getenv):
		return "go install github.com/EmoFa/tsuzuki/cmd/tsuzuki@latest"
	case goos == "linux" && strings.HasPrefix(slash, "/usr/bin/"):
		if exists("/etc/arch-release") {
			return "update tsuzuki-bin with your AUR helper, e.g. yay -Syu"
		}
		return "update it with your package manager"
	}
	return "download it from " + ReleasesURL
}

func isGoBin(exe string, getenv func(string) string) bool {
	var dirs []string
	if d := getenv("GOBIN"); d != "" {
		dirs = append(dirs, d)
	}
	for _, p := range filepath.SplitList(getenv("GOPATH")) {
		if p != "" {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	}
	if home := getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	if profile := getenv("USERPROFILE"); profile != "" {
		dirs = append(dirs, filepath.Join(profile, "go", "bin"))
	}
	for _, d := range dirs {
		if strings.EqualFold(filepath.ToSlash(filepath.Dir(filepath.FromSlash(exe))), filepath.ToSlash(d)) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
