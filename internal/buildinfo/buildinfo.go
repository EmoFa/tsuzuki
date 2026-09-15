// Package buildinfo exposes version metadata injected at build time via
// -ldflags "-X github.com/EmoFa/tsuzuki/internal/buildinfo.Version=...".
package buildinfo

import "runtime/debug"

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func init() {
	if Commit == "none" { // goreleaser's placeholder when git has no tags
		Commit = ""
	}
	if Commit != "" {
		return
	}
	// Fall back to VCS info embedded by `go build` in a git checkout.
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				Commit = s.Value
			case "vcs.time":
				Date = s.Value
			}
		}
	}
}
