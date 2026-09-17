package player

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMpvBinary creates an executable file named like mpv in dir.
func fakeMpvBinary(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, mpvName)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindMpvConfigured(t *testing.T) {
	dir := t.TempDir()
	mpv := fakeMpvBinary(t, dir)

	for name, configured := range map[string]string{"file": mpv, "folder": dir} {
		if got, err := FindMpv(configured); err != nil || got != mpv {
			t.Errorf("%s: FindMpv(%q) = %q, %v", name, configured, got, err)
		}
	}

	// A bare name is looked up on the PATH.
	t.Setenv("PATH", dir)
	if got, err := FindMpv("mpv"); err != nil || !strings.EqualFold(filepath.Clean(got), filepath.Clean(mpv)) {
		t.Errorf("bare name: %q, %v", got, err)
	}

	missing := filepath.Join(dir, "nope", mpvName)
	_, err := FindMpv(missing)
	if !errors.Is(err, ErrMpvNotFound) || !strings.Contains(err.Error(), "player.mpv_path") {
		t.Errorf("missing: %v", err)
	}
}

func TestFindMpvOnPath(t *testing.T) {
	dir := t.TempDir()
	mpv := fakeMpvBinary(t, dir)
	t.Setenv("PATH", dir)
	if got, err := FindMpv(""); err != nil || !strings.EqualFold(filepath.Clean(got), filepath.Clean(mpv)) {
		t.Errorf("FindMpv = %q, %v", got, err)
	}
}

func TestFindMpvNotFoundListsPlaces(t *testing.T) {
	for _, c := range mpvCandidates() {
		if isFile(c) {
			t.Skipf("mpv installed at %s", c)
		}
	}
	t.Setenv("PATH", t.TempDir())
	_, err := FindMpv("")
	if !errors.Is(err, ErrMpvNotFound) {
		t.Fatalf("err = %v", err)
	}
	if c := mpvCandidates(); len(c) > 0 && !strings.Contains(err.Error(), filepath.Dir(c[0])) {
		t.Errorf("error doesn't list %s:\n%v", filepath.Dir(c[0]), err)
	}
	if !strings.Contains(err.Error(), "player.mpv_path") {
		t.Errorf("error doesn't mention player.mpv_path:\n%v", err)
	}
}

func TestFindMpvEscapedPathHint(t *testing.T) {
	_, err := FindMpv("C:\tools\\mpv.exe") // what "C:\tools\mpv.exe" in TOML double quotes becomes
	if !errors.Is(err, ErrMpvNotFound) || !strings.Contains(err.Error(), "single quotes") {
		t.Fatalf("err = %v", err)
	}
}
