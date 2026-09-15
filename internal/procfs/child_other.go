//go:build !linux

package procfs

import "os/exec"

// DieWithParent is a no-op where the OS has no parent-death signal.
func DieWithParent(cmd *exec.Cmd) {}
