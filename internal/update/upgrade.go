package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DownloadBase is where release archives are published.
const DownloadBase = "https://github.com/EmoFa/tsuzuki/releases/download"

const (
	// maxDownload bounds an archive, which is about 12MB compressed.
	maxDownload = 200 << 20
	// downloadTimeout bounds the whole download.
	downloadTimeout = 10 * time.Minute
	// versionTimeout bounds the check that the new binary runs.
	versionTimeout = 20 * time.Second
)

// ErrNotSelfUpgradable means something else installed this binary, so it
// should be upgraded the way it was installed.
var ErrNotSelfUpgradable = errors.New("this tsuzuki wasn't installed by hand")

// Upgrader replaces the running binary with a release from GitHub.
type Upgrader struct {
	Client  *http.Client // nil: a client with a download timeout
	Current string       // the running version
	Install Install
	// Progress is called as the archive downloads; total is 0 when the server
	// doesn't say how big it is.
	Progress func(done, total int64)
	// Base is the release download URL, for tests.
	Base string
	// checkVersion runs the new binary to see what it reports, for tests.
	checkVersion func(exe string) (string, error)
}

// Check reports whether this binary can be replaced at all: nothing is
// downloaded when it can't be.
func (u *Upgrader) Check() error {
	if !IsRelease(u.Current) {
		return fmt.Errorf("%s is a build from source, not a release: build it again instead", u.Current)
	}
	if !u.Install.SelfUpgrades() {
		return fmt.Errorf("%w: upgrade it with `%s`", ErrNotSelfUpgradable, u.Install.Command())
	}
	if err := writable(filepath.Dir(u.Install.Exe)); err != nil {
		return fmt.Errorf("%s can't be replaced: %w", u.Install.Exe, err)
	}
	return nil
}

// Upgrade downloads a release, checks it, and puts it in place of the running
// binary. It reports the version now installed.
func (u *Upgrader) Upgrade(ctx context.Context, rel Release) (string, error) {
	if err := u.Check(); err != nil {
		return "", err
	}
	exe := u.Install.Exe
	dir := filepath.Dir(exe)

	name := ArchiveName(rel.Version, runtime.GOOS, runtime.GOARCH)
	base := u.Base
	if base == "" {
		base = DownloadBase
	}
	base = strings.TrimRight(base, "/") + "/v" + rel.Version

	archive, err := u.download(ctx, base+"/"+name, dir, u.Progress)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)

	sums, err := u.download(ctx, base+"/checksums.txt", dir, nil)
	if err != nil {
		return "", err
	}
	defer os.Remove(sums)
	if err := verify(archive, sums, name); err != nil {
		return "", err
	}

	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm()
	}
	binary, err := extract(archive, name, dir, binaryName(), mode)
	if err != nil {
		return "", err
	}
	defer os.Remove(binary)

	check := u.checkVersion
	if check == nil {
		check = reportedVersion
	}
	got, err := check(binary)
	if err != nil {
		return "", fmt.Errorf("the downloaded tsuzuki wouldn't run: %w", err)
	}
	if !strings.Contains(got, rel.Version) {
		return "", fmt.Errorf("the downloaded tsuzuki reports %q, not %s", got, rel.Version)
	}
	if err := replace(binary, exe); err != nil {
		return "", fmt.Errorf("replacing %s: %w", exe, err)
	}
	return rel.Version, nil
}

// ArchiveName is the release archive for a platform, as goreleaser names them.
func ArchiveName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("tsuzuki_%s_%s_%s%s", version, goos, goarch, ext)
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "tsuzuki.exe"
	}
	return "tsuzuki"
}

// writable reports whether files can be created in dir, which is where both
// the download and the replacement happen.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".tsuzuki-write-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("no permission to write to %s; run the upgrade as a user who can, or install tsuzuki with a package manager", dir)
		}
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// download streams a URL to a temp file beside the binary, so the replacement
// is a rename within one filesystem.
func (u *Upgrader) download(ctx context.Context, url, dir string, progress func(done, total int64)) (string, error) {
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", path.Base(url), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", path.Base(url), resp.Status)
	}

	f, err := os.CreateTemp(dir, ".tsuzuki-download-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if progress == nil {
		progress = func(int64, int64) {}
	}
	body := &progressReader{r: io.LimitReader(resp.Body, maxDownload+1), total: resp.ContentLength, report: progress}
	n, err := io.Copy(f, body)
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("downloading %s: %w", path.Base(url), err)
	}
	if n > maxDownload {
		os.Remove(f.Name())
		return "", fmt.Errorf("downloading %s: larger than %d bytes", path.Base(url), int64(maxDownload))
	}
	return f.Name(), nil
}

type progressReader struct {
	r      io.Reader
	done   int64
	total  int64
	report func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.report(p.done, p.total)
	return n, err
}

// verify checks the archive against the release's checksums file.
func verify(archive, sums, name string) error {
	want, err := checksumFor(sums, name)
	if err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%s doesn't match its checksum (got %s, want %s); nothing was replaced", name, got, want)
	}
	return nil
}

// checksumFor reads the "<sha256>  <file>" line for name.
func checksumFor(sums, name string) (string, error) {
	data, err := os.ReadFile(sums)
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(data)) {
		sum, file, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if strings.TrimSpace(file) == name {
			return sum, nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in the release", name)
}

// extract writes the named binary out of the archive, beside the old one.
// asset is the release's file name, which says how the archive is packed.
func extract(archive, asset, dir, name string, mode os.FileMode) (string, error) {
	out, err := os.CreateTemp(dir, ".tsuzuki-new-*")
	if err != nil {
		return "", err
	}
	defer out.Close()
	unpack := untar
	if strings.HasSuffix(asset, ".zip") {
		unpack = unzip
	}
	if err := unpack(archive, name, out); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	if err := out.Chmod(mode); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

func untar(archive, name string, out io.Writer) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading the archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		// Only the binary at the archive root, never a path out of it.
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != name || strings.Contains(h.Name, "/") {
			continue
		}
		_, err = io.Copy(out, io.LimitReader(tr, maxDownload))
		return err
	}
	return fmt.Errorf("the archive holds no %s", name)
}

func unzip(archive, name string, out io.Writer) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("reading the archive: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != name || strings.Contains(f.Name, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		defer rc.Close()
		_, err = io.Copy(out, io.LimitReader(rc, maxDownload))
		return err
	}
	return fmt.Errorf("the archive holds no %s", name)
}

// reportedVersion asks a binary what version it is, which is the last check
// before it replaces the running one.
func reportedVersion(exe string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
