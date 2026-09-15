package procfs

import (
	"os/exec"
	"syscall"
)

// DieWithParent makes the child receive SIGTERM if tsuzuki dies without cleaning
// up (SIGKILL, a crash), so mpv or a browser isn't left running.
func DieWithParent(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
