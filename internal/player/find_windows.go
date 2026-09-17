//go:build windows

package player

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	// mpv.exe rather than "mpv", which would pick the mpv.com console wrapper first.
	mpvName     = "mpv.exe"
	mpvPathHint = "\nIn config.toml, quote Windows paths with single quotes: mpv_path = 'C:\\path\\to\\mpv.exe'"
)

// mpvCandidates lists places mpv is installed without being on this process's
// PATH, most specific first.
func mpvCandidates() []string {
	home, _ := os.UserHomeDir()
	return windowsCandidates(os.Getenv, registryString, executableDir(), home)
}

// windowsCandidates builds the candidate list from its inputs, for testing.
func windowsCandidates(getenv func(string) string, reg func(root registry.Key, path, name string) string, exeDir, home string) []string {
	var out []string
	add := func(dir string, parts ...string) {
		if dir != "" {
			out = append(out, filepath.Join(append([]string{dir}, parts...)...))
		}
	}

	// Next to tsuzuki.exe. (Go won't run a program found in the current
	// directory through the PATH, so this has to be explicit.)
	add(exeDir, mpvName)

	// `mpv --register` and mpv-install.bat record mpv.exe under App Paths, which
	// the Run dialog uses but a PATH search doesn't.
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		if p := reg(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\mpv.exe`, ""); p != "" {
			out = append(out, strings.Trim(p, `"`))
		}
	}

	// The PATH as currently saved: a terminal opened before mpv was added to
	// the PATH still has the old one.
	for _, key := range []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		for _, dir := range filepath.SplitList(reg(key.root, key.path, "Path")) {
			add(strings.Trim(dir, `"`), mpvName)
		}
	}

	// Common installers.
	programFiles := getenv("ProgramFiles")
	add(programFiles, "mpv", mpvName)
	add(programFiles, "MPV Player", mpvName) // winget's shinchiro.mpv
	localAppData := getenv("LocalAppData")
	add(localAppData, "Programs", "mpv", mpvName)
	add(localAppData, "Microsoft", "WinGet", "Links", mpvName) // winget portable packages
	scoop := getenv("SCOOP")
	if scoop == "" && home != "" {
		scoop = filepath.Join(home, "scoop")
	}
	add(scoop, "apps", "mpv", "current", mpvName)
	add(scoop, "shims", mpvName)
	chocolatey := getenv("ChocolateyInstall")
	if chocolatey == "" {
		chocolatey = `C:\ProgramData\chocolatey`
	}
	add(chocolatey, "bin", mpvName)
	return out
}

// registryString reads a string value (expanding %VARS%), or "" when absent.
func registryString(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close() //nolint:errcheck // read-only key
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	if expanded, err := registry.ExpandString(v); err == nil {
		v = expanded
	}
	return v
}
