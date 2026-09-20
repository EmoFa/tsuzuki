package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/httpx"
)

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"0.3.0":                   true,
		"v0.3.0":                  true,
		"v0.3.0-dirty":            false,
		"v0.2.0-9-gc5331a3-dirty": false,
		"v0.3.0-1-g756b87c":       false,
		"dev":                     false,
		"":                        false,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v", v, got)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "v0.1.1", true},
		{"0.9.9", "1.0.0", true},
		{"0.10.0", "0.9.0", false},
		{"0.2.0", "0.2.0", false},
		{"0.3.0", "0.2.0", false},
		{"dev", "0.2.0", false},
		{"0.1.1-next", "0.2.0", false},
		{"037d54b-dirty", "0.2.0", false},
		{"0.1.0", "0.2.0-rc1", false},
	} {
		if got := Newer(tc.current, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.current, tc.latest, got)
		}
	}
}

func TestDetectAndUpgradeCommand(t *testing.T) {
	env := map[string]string{"HOME": "/home/u", "GOPATH": "/work/go", "USERPROFILE": `C:\Users\u`}
	getenv := func(k string) string { return env[k] }
	for _, tc := range []struct {
		exe, goos string
		arch      bool
		kind      InstallKind
		want      string
	}{
		{"/opt/homebrew/Caskroom/tsuzuki/0.1.0/tsuzuki", "darwin", false, KindHomebrew, "brew upgrade tsuzuki"},
		{"/home/linuxbrew/.linuxbrew/Caskroom/tsuzuki/0.1.0/tsuzuki", "linux", false, KindHomebrew, "brew upgrade tsuzuki"},
		{`C:\Users\u\AppData\Local\Microsoft\WinGet\Packages\EmoFa.tsuzuki_x\tsuzuki.exe`, "windows", false, KindWinget, "winget upgrade EmoFa.tsuzuki"},
		{"/home/u/go/bin/tsuzuki", "linux", false, KindGo, "go install github.com/EmoFa/tsuzuki/cmd/tsuzuki@latest"},
		{"/work/go/bin/tsuzuki", "linux", false, KindGo, "go install github.com/EmoFa/tsuzuki/cmd/tsuzuki@latest"},
		{"/usr/bin/tsuzuki", "linux", true, KindSystemArch, "update tsuzuki-bin with your AUR helper, e.g. yay -Syu"},
		{"/usr/bin/tsuzuki", "linux", false, KindSystem, "update it with your package manager"},
		{`C:\Users\u\Downloads\tsuzuki.exe`, "windows", false, KindManual, "tsuzuki upgrade"},
		{"/home/u/apps/tsuzuki", "linux", false, KindManual, "tsuzuki upgrade"},
	} {
		exists := func(string) bool { return tc.arch }
		in := detect(tc.exe, tc.goos, getenv, exists)
		if in.Kind != tc.kind {
			t.Errorf("%s: kind %v, want %v", tc.exe, in.Kind, tc.kind)
		}
		if got := in.Command(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.exe, got, tc.want)
		}
		// Only a binary nothing else manages may be replaced in place.
		if got := in.SelfUpgrades(); got != (tc.kind == KindManual) {
			t.Errorf("%s: SelfUpgrades = %v", tc.exe, got)
		}
	}
}

type memCache struct {
	values  map[string]string
	updated time.Time
}

func (m *memCache) GetKV(_ context.Context, k string) (string, time.Time, bool, error) {
	v, ok := m.values[k]
	return v, m.updated, ok, nil
}

func (m *memCache) PutKV(_ context.Context, k, v string) error {
	m.values[k] = v
	m.updated = time.Now()
	return nil
}

func TestLatestIsCachedForADay(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://github.com/EmoFa/tsuzuki/releases/tag/v0.2.0","prerelease":false}`))
	}))
	defer srv.Close()

	cache := &memCache{values: map[string]string{}}
	c := &Checker{Client: httpx.New(httpx.Options{Retries: -1}), Cache: cache, URL: srv.URL}
	ctx := context.Background()
	for range 2 {
		r, err := c.Latest(ctx)
		if err != nil || r.Version != "0.2.0" || r.URL == "" {
			t.Fatalf("Latest = %+v, %v", r, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("GitHub hit %d times, want 1 (cached)", hits.Load())
	}

	cache.updated = time.Now().Add(-25 * time.Hour)
	if _, err := c.Latest(ctx); err != nil || hits.Load() != 2 {
		t.Fatalf("stale cache: hits=%d err=%v", hits.Load(), err)
	}
}
