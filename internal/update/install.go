package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallKind is how a copy of tsuzuki was installed, which decides how it
// should be upgraded.
type InstallKind int

const (
	// KindManual is a binary unpacked from a release archive, which nothing
	// else keeps up to date, so tsuzuki can replace it itself.
	KindManual InstallKind = iota
	KindHomebrew
	KindWinget
	KindGo
	// KindSystem is a distribution package: apt, rpm, the AUR.
	KindSystem
	// KindSystemArch is a system package on Arch, where tsuzuki-bin comes from
	// the AUR and needs a helper rather than pacman.
	KindSystemArch
)

// Install describes the running tsuzuki.
type Install struct {
	Kind InstallKind
	Exe  string
}

// Detect works out how the binary at exe was installed.
func Detect(exe string) Install {
	return detect(exe, runtime.GOOS, os.Getenv, fileExists)
}

func detect(exe, goos string, getenv func(string) string, exists func(string) bool) Install {
	slash := strings.ReplaceAll(exe, `\`, "/") // Windows paths, whatever OS runs this
	lower := strings.ToLower(slash)
	in := Install{Exe: exe}
	switch {
	case strings.Contains(lower, "/caskroom/") || strings.Contains(lower, "/cellar/") ||
		strings.Contains(lower, "/homebrew/") || strings.Contains(lower, "/.linuxbrew/") ||
		(getenv("HOMEBREW_PREFIX") != "" && strings.HasPrefix(slash, filepath.ToSlash(getenv("HOMEBREW_PREFIX"))+"/")):
		in.Kind = KindHomebrew
	case goos == "windows" && strings.Contains(lower, "/winget/"):
		in.Kind = KindWinget
	case isGoBin(slash, getenv):
		in.Kind = KindGo
	case goos == "linux" && strings.HasPrefix(slash, "/usr/bin/"):
		in.Kind = KindSystem
		if exists("/etc/arch-release") {
			in.Kind = KindSystemArch
		}
	default:
		in.Kind = KindManual
	}
	return in
}

// SelfUpgrades reports whether tsuzuki may replace this binary itself. Only a
// manual install qualifies: everything else belongs to a package manager,
// which keeps its own record of what it installed.
func (i Install) SelfUpgrades() bool { return i.Kind == KindManual }

// Command says how to upgrade this installation.
func (i Install) Command() string {
	switch i.Kind {
	case KindHomebrew:
		return "brew upgrade tsuzuki"
	case KindWinget:
		return "winget upgrade EmoFa.tsuzuki"
	case KindGo:
		return "go install github.com/EmoFa/tsuzuki/cmd/tsuzuki@latest"
	case KindSystemArch:
		return "update tsuzuki-bin with your AUR helper, e.g. yay -Syu"
	case KindSystem:
		return "update it with your package manager"
	}
	return "tsuzuki upgrade"
}

// UpgradeCommand says how to upgrade a tsuzuki installed at exe.
func UpgradeCommand(exe string) string { return Detect(exe).Command() }

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
