//go:build unix

package cli

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"aka/internal/daemon"
)

// TestMain lets the test binary act as aka itself (AKA_TEST_MAIN=1), so
// `aka up` can re-execute it for each app, and for the daemon.
func TestMain(m *testing.M) {
	if os.Getenv("AKA_TEST_MAIN") == "1" {
		os.Exit(Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestUp(t *testing.T) {
	state, err := os.MkdirTemp("", "akau")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Stop(state, 0); os.RemoveAll(state) })
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	proj := t.TempDir()
	os.Mkdir(filepath.Join(proj, ".git"), 0o755)
	os.MkdirAll(filepath.Join(proj, "svc"), 0o755)
	os.WriteFile(filepath.Join(proj, "aka.toml"), []byte(`
[apps]
web = "sh -c 'echo hello from web; sleep 0.3'"

[apps.api]
cmd = "sh -c 'echo api in $(basename $PWD) on $PORT; exit 3'"
dir = "svc"
`), 0o644)

	exe, _ := os.Executable()
	run := func(args ...string) (string, int) {
		cmd := exec.Command(exe, args...)
		cmd.Dir = proj
		cmd.Env = append(os.Environ(), "AKA_TEST_MAIN=1", "AKA_STATE_DIR="+state,
			"AKA_PROXY_PORT="+strconv.Itoa(port), "AKA_HTTPS=0", "AKA_HTTPS_PORT=0")
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code
	}

	out, code := run("up")
	if code != 3 {
		t.Errorf("exit %d, want 3 (api's code)\n%s", code, out)
	}
	for _, want := range []string{"web | hello from web", "api | api in svc on ", "api exited with code 3", "web | [aka] http://web.localhost:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// A bare aka runs the project file too; naming an app runs just it.
	out, code = run("up", "web")
	if code != 0 || strings.Contains(out, "api |") {
		t.Errorf("up web: exit %d\n%s", code, out)
	}
	if out, _ := run(); !strings.Contains(out, "starting api, web from") {
		t.Errorf("bare aka did not use aka.toml:\n%s", out)
	}
	if out, code := run("up", "nope"); code == 0 || !strings.Contains(out, `has no app "nope"`) {
		t.Errorf("unknown app: %d %s", code, out)
	}
}
