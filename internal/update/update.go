// Package update checks GitHub for a newer tsuzuki release and says how to
// upgrade the way it was installed. It never downloads or replaces anything.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// IsRelease reports whether v is a released version rather than a build from
// source, which carries a "-dirty" or "-<n>-g<commit>" suffix.
func IsRelease(v string) bool {
	_, ok := parse(v)
	return ok
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
