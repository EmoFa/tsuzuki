// Package procfs cleans up temporary files that tsuzuki processes create with
// their PID in the name, once those processes are gone (killed or crashed).
package procfs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Name returns prefix + this process's PID + "-", for names SweepStale understands.
func Name(prefix string) string {
	return prefix + strconv.Itoa(os.Getpid()) + "-"
}

// SweepStale removes entries in dir named prefix + PID + "-…" whose process no
// longer exists. Entries of running processes (including this one) are kept.
func SweepStale(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok {
			continue
		}
		pidText, _, ok := strings.Cut(rest, "-")
		pid, err := strconv.Atoi(pidText)
		if !ok || err != nil || pid <= 0 || pid == os.Getpid() || Alive(pid) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			slog.Debug("removing stale temp file", "path", path, "err", err)
			continue
		}
		slog.Debug("removed stale temp file", "path", path)
	}
}
