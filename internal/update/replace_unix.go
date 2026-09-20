//go:build !windows

package update

import "os"

// replace puts the new binary in place of the old one. Unix lets a running
// binary be renamed over, since the process holds the open file, not the name.
func replace(newBinary, exe string) error {
	return os.Rename(newBinary, exe)
}

// CleanupOld removes anything an earlier upgrade left behind. Only Windows
// leaves anything.
func CleanupOld(string) {}
