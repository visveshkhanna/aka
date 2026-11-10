//go:build !unix

package daemon

import "os"

func signalPIDs(pids []int, _ bool) {
	for _, p := range pids {
		if pr, err := os.FindProcess(p); err == nil {
			pr.Kill()
		}
	}
}
