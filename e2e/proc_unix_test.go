//go:build e2e && unix

package e2e

import (
	"os"
	"os/exec"
	"syscall"
)

// setGroup keeps aka out of the test's process group, so only the signals
// the test sends reach it.
func setGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func interrupt(cmd *exec.Cmd) { cmd.Process.Signal(os.Interrupt) }
