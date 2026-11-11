//go:build unix

package runner

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

var (
	termSignal os.Signal = syscall.SIGTERM
	killSignal os.Signal = syscall.SIGKILL
)

// isolate starts the child in a new session (and so a new process group,
// whose ID is the child's PID). The child's tree then has a process group of
// its own, which makes "same group" a reliable membership test for detection
// and lets cleanup signal the whole tree at once. The terminal's Ctrl+C
// reaches only aka, which forwards it exactly once. Stdin is still the
// terminal's file descriptor, so prompts and keyboard shortcuts keep working.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// childGroup returns the process group to watch for the child.
func childGroup(pid int) int { return pid }

// notifySignals must be called before the child starts: a signal with a Go
// handler resets to default in the child at exec, while one still ignored is
// inherited as ignored. Signals that were ignored when aka started (nohup, a
// background job in a script) stay ignored for aka and the child alike,
// exactly as if the command had been run directly.
func notifySignals() chan os.Signal {
	ch := make(chan os.Signal, 4)
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		if !signal.Ignored(sig) {
			signal.Notify(ch, sig)
		}
	}
	return ch
}

func stopSignals(ch chan os.Signal) { signal.Stop(ch) }

// signalGroup signals every process in group pgid.
func signalGroup(pgid int, sig os.Signal) {
	if s, ok := sig.(syscall.Signal); ok && pgid > 0 {
		syscall.Kill(-pgid, s)
	}
}

func signalExitCode(ee *exec.ExitError) int {
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 0
}

// releaseGroup is a no-op on Unix: a process group needs no handle.
func releaseGroup(int) {}
