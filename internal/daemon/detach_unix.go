//go:build unix

package daemon

import (
	"os/exec"
	"syscall"
)

// detach starts the daemon in its own session so it survives the terminal
// that launched it and never receives that terminal's Ctrl+C.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
