//go:build unix

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

var upSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

// upIsolate gives each app's aka its own process group, so a terminal
// Ctrl+C reaches only `aka up`, which forwards it to each exactly once
// (many dev servers treat a second Ctrl+C as "quit now").
func upIsolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func upForward(p *os.Process, sig os.Signal) { p.Signal(sig) }
