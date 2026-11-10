//go:build unix

package daemon

import "syscall"

func signalPIDs(pids []int, kill bool) {
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	for _, p := range pids {
		syscall.Kill(p, sig)
	}
}
