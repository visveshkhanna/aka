package daemon

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach starts the daemon without a console and outside the CLI's console
// process group, so closing the terminal or pressing Ctrl+C there does not
// stop it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    true,
	}
}
