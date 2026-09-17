//go:build !windows

package player

import (
	"path/filepath"
	"runtime"
)

const (
	mpvName     = "mpv"
	mpvPathHint = ""
)

// mpvCandidates lists places mpv is installed without being on the PATH.
func mpvCandidates() []string {
	var out []string
	if runtime.GOOS == "darwin" {
		out = append(out, "/opt/homebrew/bin/mpv", "/usr/local/bin/mpv", "/Applications/mpv.app/Contents/MacOS/mpv")
	}
	if dir := executableDir(); dir != "" {
		out = append(out, filepath.Join(dir, mpvName))
	}
	return out
}
