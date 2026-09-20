package update

import (
	"os"
)

// oldSuffix names the binary an upgrade displaced. Windows won't let a running
// executable be overwritten, but it will let it be renamed, so the old one is
// moved aside and deleted the next time tsuzuki starts.
const oldSuffix = ".old"

func replace(newBinary, exe string) error {
	old := exe + oldSuffix
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newBinary, exe); err != nil {
		// Put the running binary back, so this one stays usable.
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

// CleanupOld removes the binary an earlier upgrade displaced. It stays behind
// while that tsuzuki is still running, so failure here is normal and ignored.
func CleanupOld(exe string) {
	if exe != "" {
		_ = os.Remove(exe + oldSuffix)
	}
}
