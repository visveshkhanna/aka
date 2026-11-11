//go:build unix

package cli

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"

	"github.com/visveshkhanna/aka/internal/daemon"
)

// TestConcurrentStart starts the proxy from several aka processes at once,
// as when apps are launched together with no proxy running. Exactly one
// daemon wins the lock; every caller must end up using it, not fail
// because the loser it spawned exited first.
func TestConcurrentStart(t *testing.T) {
	for round := 0; round < 3; round++ {
		state, err := os.MkdirTemp("", "akar")
		if err != nil {
			t.Fatal(err)
		}
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		exe, _ := os.Executable()

		const n = 6
		var wg sync.WaitGroup
		outs, errs := make([]string, n), make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				cmd := exec.Command(exe, "proxy", "start")
				// HTTPS on: the winner creates the CA before it listens,
				// which widens the window the losers must wait through.
				cmd.Env = append(os.Environ(), "AKA_TEST_MAIN=1", "AKA_STATE_DIR="+state,
					"AKA_PROXY_PORT="+strconv.Itoa(port), "AKA_HTTPS_PORT=0")
				out, err := cmd.CombinedOutput()
				outs[i], errs[i] = string(out), err
			}(i)
		}
		wg.Wait()
		daemon.Stop(state, 0)
		os.RemoveAll(state)
		for i := range errs {
			if errs[i] != nil {
				t.Fatalf("round %d, starter %d: %v\n%s", round, i, errs[i], outs[i])
			}
		}
	}
}
