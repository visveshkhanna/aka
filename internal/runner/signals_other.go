//go:build !unix && !windows

package runner

import (
	"os"
	"os/exec"
	"os/signal"
)

var (
	termSignal os.Signal = os.Kill // Windows has no SIGTERM; proper handling is the Windows step
	killSignal os.Signal = os.Kill
)

func isolate(*exec.Cmd)                  {}
func childGroup(int) int                 { return 0 }
func signalGroup(int, os.Signal)         {}
func releaseGroup(int)                   {}
func signalExitCode(*exec.ExitError) int { return 0 }

func notifySignals() chan os.Signal {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt)
	return ch
}

func stopSignals(ch chan os.Signal) { signal.Stop(ch) }
