package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// releaseServer serves a release the way GitHub does: an archive holding the
// binary, and a checksums file listing it.
func releaseServer(t *testing.T, version, binary string, corrupt bool) *httptest.Server {
	t.Helper()
	files := map[string]string{binaryName(): binary, "README.md": "docs"}
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		for name, body := range files {
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := errors.Join(tw.Close(), gz.Close()); err != nil {
			t.Fatal(err)
		}
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if corrupt {
		digest = strings.Repeat("0", len(digest))
	}
	asset := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	sums := fmt.Sprintf("%s  %s\n%s  tsuzuki_%s_other.tar.gz\n", digest, asset, digest, version)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			w.Write([]byte(sums))
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

// installed writes a stub binary and returns a manual install pointing at it.
func installed(t *testing.T, content string) Install {
	t.Helper()
	exe := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return Install{Kind: KindManual, Exe: exe}
}

func TestUpgradeReplacesTheBinary(t *testing.T) {
	srv := releaseServer(t, "0.4.0", "new binary", false)
	defer srv.Close()
	in := installed(t, "old binary")

	var seen int64
	u := &Upgrader{
		Current: "0.3.1", Install: in, Base: srv.URL,
		Progress:     func(done, _ int64) { seen = done },
		checkVersion: func(string) (string, error) { return "tsuzuki 0.4.0", nil },
	}
	got, err := u.Upgrade(context.Background(), Release{Version: "0.4.0"})
	if err != nil || got != "0.4.0" {
		t.Fatalf("Upgrade = %q, %v", got, err)
	}
	if b, _ := os.ReadFile(in.Exe); string(b) != "new binary" {
		t.Errorf("binary is %q", b)
	}
	fi, err := os.Stat(in.Exe)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no executable bit: Go maps the read-only attribute onto the
	// mode, so a writable file always reads as 0666 there.
	if want := os.FileMode(0o755); runtime.GOOS != "windows" && fi.Mode().Perm() != want {
		t.Errorf("mode = %v, want %v", fi.Mode().Perm(), want)
	}
	if seen == 0 {
		t.Error("no progress reported")
	}
	// Downloads and extractions clean up after themselves; only the displaced
	// binary Windows can't delete yet may remain.
	entries, _ := os.ReadDir(filepath.Dir(in.Exe))
	for _, e := range entries {
		if e.Name() != binaryName() && !strings.HasSuffix(e.Name(), ".old") {
			t.Errorf("left %s behind", e.Name())
		}
	}
}

func TestUpgradeRefusals(t *testing.T) {
	ctx := context.Background()
	rel := Release{Version: "0.4.0"}

	t.Run("a bad checksum keeps the old binary", func(t *testing.T) {
		srv := releaseServer(t, "0.4.0", "new binary", true)
		defer srv.Close()
		in := installed(t, "old binary")
		u := &Upgrader{Current: "0.3.1", Install: in, Base: srv.URL,
			checkVersion: func(string) (string, error) { return "tsuzuki 0.4.0", nil }}
		if _, err := u.Upgrade(ctx, rel); err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("err = %v", err)
		}
		if b, _ := os.ReadFile(in.Exe); string(b) != "old binary" {
			t.Errorf("binary is %q", b)
		}
	})

	t.Run("a binary reporting the wrong version isn't used", func(t *testing.T) {
		srv := releaseServer(t, "0.4.0", "new binary", false)
		defer srv.Close()
		in := installed(t, "old binary")
		u := &Upgrader{Current: "0.3.1", Install: in, Base: srv.URL,
			checkVersion: func(string) (string, error) { return "tsuzuki 0.1.0", nil }}
		if _, err := u.Upgrade(ctx, rel); err == nil || !strings.Contains(err.Error(), "reports") {
			t.Fatalf("err = %v", err)
		}
		if b, _ := os.ReadFile(in.Exe); string(b) != "old binary" {
			t.Errorf("binary is %q", b)
		}
	})

	t.Run("a missing release says so", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(http.NotFound))
		defer srv.Close()
		u := &Upgrader{Current: "0.3.1", Install: installed(t, "old"), Base: srv.URL}
		if _, err := u.Upgrade(ctx, rel); err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a package manager's binary is left alone", func(t *testing.T) {
		in := installed(t, "old")
		in.Kind = KindHomebrew
		u := &Upgrader{Current: "0.3.1", Install: in}
		_, err := u.Upgrade(ctx, rel)
		if !errors.Is(err, ErrNotSelfUpgradable) || !strings.Contains(err.Error(), "brew upgrade tsuzuki") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a build from source is left alone", func(t *testing.T) {
		u := &Upgrader{Current: "v0.3.1-2-gabc1234-dirty", Install: installed(t, "old")}
		if _, err := u.Upgrade(ctx, rel); err == nil || !strings.Contains(err.Error(), "build from source") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestArchiveName(t *testing.T) {
	if got := ArchiveName("0.3.1", "linux", "amd64"); got != "tsuzuki_0.3.1_linux_amd64.tar.gz" {
		t.Errorf("linux: %q", got)
	}
	if got := ArchiveName("0.3.1", "windows", "arm64"); got != "tsuzuki_0.3.1_windows_arm64.zip" {
		t.Errorf("windows: %q", got)
	}
}
