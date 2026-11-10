package detect

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a tiny HTTP server when re-executed by the live test.
func TestMain(m *testing.M) {
	if os.Getenv("DETECT_TEST_SERVE") == "1" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Println("listen error:", err)
			os.Exit(1)
		}
		// A second, non-HTTP listener, as a debugger port would be.
		raw, _ := net.Listen("tcp", "127.0.0.1:0")
		fmt.Printf("PORTS %d %d\n", ln.Addr().(*net.TCPAddr).Port, raw.Addr().(*net.TCPAddr).Port)
		http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
		return
	}
	os.Exit(m.Run())
}

// TestLiveGrandchildListener spawns `sh -c <test binary>` so the server is a
// grandchild of the test, then checks the real OS detector finds its socket
// through the process tree, and the probe picks the HTTP one.
func TestLiveGrandchildListener(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("listener detection not implemented on " + runtime.GOOS)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// "; exit" stops sh from exec-ing the last command directly, which some
	// shells do and which would make the server a child rather than grandchild.
	cmd := exec.Command("/bin/sh", "-c", strconv.Quote(exe)+"; exit $?")
	cmd.Env = append(os.Environ(), "DETECT_TEST_SERVE=1")
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Kill the whole tree: the grandchild would otherwise outlive the test.
		procs, _ := ListProcs()
		for _, pid := range Tree(procs, cmd.Process.Pid, 0, 0) {
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}
		cmd.Wait()
	}()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading child output: %v", err)
	}
	var httpPort, rawPort int
	if _, err := fmt.Sscanf(strings.TrimSpace(line), "PORTS %d %d", &httpPort, &rawPort); err != nil {
		t.Fatalf("unexpected child output %q", line)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		procs, err := ListProcs()
		if err != nil {
			t.Fatal(err)
		}
		tree := Tree(procs, cmd.Process.Pid, 0, os.Getpid())
		ls, err := Listeners(tree)
		if err != nil {
			t.Fatal(err)
		}
		ports := Ports(ls)
		if contains(ports, httpPort) && contains(ports, rawPort) {
			if len(tree) < 2 {
				t.Fatalf("expected sh plus grandchild in tree, got %v", tree)
			}
			choice, ok := Choose(Input{Listeners: ls, Probe: func(l Listener) bool { return ProbeHTTP(l, time.Second) }})
			if !ok || choice.Listener.Port != httpPort {
				t.Fatalf("Choose = %+v %v, want HTTP port %d (raw %d)", choice, ok, httpPort, rawPort)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("listeners %v of tree %v do not include %d and %d", ports, tree, httpPort, rawPort)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
