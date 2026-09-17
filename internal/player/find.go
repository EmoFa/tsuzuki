package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrMpvNotFound means no mpv binary could be found.
var ErrMpvNotFound = errors.New("mpv not found")

// FindMpv resolves the mpv binary from player.mpv_path (a file or the folder
// containing it), the PATH, or where installers commonly put it.
func FindMpv(configured string) (string, error) {
	if configured != "" {
		return findConfigured(configured)
	}
	if p, err := exec.LookPath(mpvName); err == nil {
		return p, nil
	}
	searched := mpvCandidates()
	for _, c := range searched {
		if isFile(c) {
			return c, nil
		}
	}
	return "", notFound(searched)
}

func findConfigured(configured string) (string, error) {
	p := configured
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		p = filepath.Join(p, mpvName)
	}
	if isFile(p) {
		return p, nil
	}
	// A bare name such as "mpv" is looked up on the PATH.
	if !strings.ContainsAny(configured, `/\`) {
		if found, err := exec.LookPath(configured); err == nil {
			return found, nil
		}
	}
	err := fmt.Errorf("player.mpv_path %q: %w (it should be mpv's full path, or the folder containing %s)", configured, ErrMpvNotFound, mpvName)
	if strings.ContainsAny(configured, "\t\n\r\b\f") {
		// "C:\tools\mpv.exe" in double quotes reads \t as a tab.
		err = fmt.Errorf("%w; backslashes in double quotes were read as escape codes, so write the path in single quotes: 'C:\\path\\to\\mpv.exe'", err)
	}
	return "", err
}

func notFound(searched []string) error {
	var b strings.Builder
	b.WriteString(" on the PATH")
	if len(searched) > 0 {
		b.WriteString(" or in:")
		seen := map[string]bool{}
		for _, c := range searched {
			if dir := filepath.Dir(c); !seen[dir] {
				seen[dir] = true
				b.WriteString("\n  " + dir)
			}
		}
		b.WriteString("\n")
	} else {
		b.WriteString(". ")
	}
	b.WriteString("Install it (https://mpv.io/installation/) or set player.mpv_path in the config.")
	return fmt.Errorf("%w%s%s", ErrMpvNotFound, b.String(), mpvPathHint)
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// executableDir is the folder tsuzuki runs from, for mpv kept alongside it.
func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}
