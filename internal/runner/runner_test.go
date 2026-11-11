package runner

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"aka/internal/brand"
	"aka/internal/daemon"
	"aka/internal/presets"
)

// TestMain doubles as the child app for the end-to-end tests. Modes:
//
//	port    listen on $PORT (an app that honours PORT)
//	fixed   ignore $PORT, listen on a random port and print its URL
//	silent  ignore $PORT, listen on a random port plus a non-HTTP port, print nothing
//
// GET /quit makes the app exit with status 7.
func TestMain(m *testing.M) {
	mode := os.Getenv("RUNNER_TEST_MODE")
	if mode == "" {
		os.Exit(m.Run())
	}
	addr := "127.0.0.1:0"
	switch mode {
	case "port":
		addr = "127.0.0.1:" + os.Getenv("PORT")
	case "flagport": // listens where --listen says, as a preset would set it
		for i, a := range os.Args {
			if a == "--listen" && i+1 < len(os.Args) {
				addr = "127.0.0.1:" + os.Args[i+1]
			}
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	switch mode {
	case "fixed":
		fmt.Printf("Server running at http://localhost:%d/\n", port)
	case "silent":
		raw, _ := net.Listen("tcp", "127.0.0.1:0") // e.g. a debugger port
		defer raw.Close()
	}
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/quit" {
			fmt.Fprint(w, "bye")
			go func() { time.Sleep(50 * time.Millisecond); os.Exit(7) }()
			return
		}
		fmt.Fprintf(w, "mode=%s port=%d env=%s host=%s url=%s preset_env=%s", mode, port, os.Getenv("PORT"), os.Getenv("HOST"), os.Getenv(brand.Env("URL")), os.Getenv("TESTAPP_ENV"))
	}))
}

func TestSubstitutePort(t *testing.T) {
	got := SubstitutePort([]string{"php", "-S", "127.0.0.1:{port}", "{port}{port}", "plain"}, 4123)
	want := []string{"php", "-S", "127.0.0.1:4123", "41234123", "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildEnv(t *testing.T) {
	base := []string{"HOME=/h", "PORT=9999", "HOST=myhost", "PATH=/old", "KEEP=1"}
	env := BuildEnv(base, 4123, "http://a.localhost:1355", "/new:/old", "", true)
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := m[k]; dup {
			t.Fatalf("duplicate key %s in %v", k, env)
		}
		m[k] = v
	}
	want := map[string]string{
		"HOME": "/h", "KEEP": "1", "PORT": "4123", "HOST": "127.0.0.1", "PATH": "/new:/old",
		brand.Env("URL"): "http://a.localhost:1355", brand.Env("PORT"): "4123", "PYTHONUNBUFFERED": "1",
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("env:\n got %v\nwant %v", m, want)
	}
	// A user-set PYTHONUNBUFFERED is respected, and a pty run adds none.
	env = BuildEnv([]string{"PYTHONUNBUFFERED=0"}, 1, "u", "p", "", true)
	if strings.Count(strings.Join(env, "\n"), "PYTHONUNBUFFERED") != 1 {
		t.Fatalf("user PYTHONUNBUFFERED overridden: %v", env)
	}
	if strings.Contains(strings.Join(BuildEnv(nil, 1, "u", "p", "", false), "\n"), "PYTHONUNBUFFERED") {
		t.Fatal("PYTHONUNBUFFERED set for pty output")
	}
}

func TestBuildEnvNodeCA(t *testing.T) {
	env := strings.Join(BuildEnv(nil, 1, "u", "p", "/s/ca.pem", false), "\n")
	if !strings.Contains(env, "NODE_EXTRA_CA_CERTS=/s/ca.pem") {
		t.Fatalf("NODE_EXTRA_CA_CERTS not set: %s", env)
	}
	env = strings.Join(BuildEnv([]string{"NODE_EXTRA_CA_CERTS=/mine.pem"}, 1, "u", "p", "/s/ca.pem", false), "\n")
	if strings.Count(env, "NODE_EXTRA_CA_CERTS") != 1 || !strings.Contains(env, "=/mine.pem") {
		t.Fatalf("user NODE_EXTRA_CA_CERTS overridden: %s", env)
	}
	if strings.Contains(strings.Join(BuildEnv(nil, 1, "u", "p", "", false), "\n"), "NODE_EXTRA_CA_CERTS") {
		t.Fatal("NODE_EXTRA_CA_CERTS set without TLS")
	}
}

func TestAugmentPathAndLookPath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "packages", "web")
	for _, d := range []string{filepath.Join(root, "node_modules", ".bin"), filepath.Join(sub, "node_modules", ".bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tool := filepath.Join(root, "node_modules", ".bin", "mytool")
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)

	p := AugmentPath("/usr/bin", sub)
	want := strings.Join([]string{filepath.Join(sub, "node_modules", ".bin"), filepath.Join(root, "node_modules", ".bin"), "/usr/bin"}, string(os.PathListSeparator))
	if p != want {
		t.Fatalf("AugmentPath = %q\nwant %q", p, want)
	}
	if got, err := LookPath("mytool", p); err != nil || got != tool {
		t.Fatalf("LookPath = %q, %v", got, err)
	}
	if _, err := LookPath("mytool", "/usr/bin"); err == nil {
		t.Fatal("found mytool without node_modules/.bin on the path")
	}
	if AugmentPath("/usr/bin", t.TempDir()) != "/usr/bin" {
		t.Fatal("PATH changed with no node_modules")
	}
}

// --- end-to-end through a real daemon and proxy ---

type e2e struct {
	t     *testing.T
	dir   string
	srv   *daemon.Server
	proxy int
	// presets, if set, are passed to Run.
	presets *presets.Set
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	dir, err := os.MkdirTemp("", "akar")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s, err := daemon.Start(daemon.Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	pollFast, pollSlow = 50*time.Millisecond, 100*time.Millisecond
	return &e2e{t: t, dir: dir, srv: s, proxy: s.ProxyPort()}
}

type result struct {
	code int
	err  error
}

// start runs Run in the background with the test binary as the child.
func (e *e2e) start(host, mode string, viaShell bool, extra ...string) (<-chan result, *os.File) {
	e.t.Helper()
	exe, _ := os.Executable()
	args := append([]string{exe}, extra...)
	if viaShell {
		// "; exit" keeps sh from exec-ing, so the app is a grandchild.
		args = []string{"/bin/sh", "-c", strconv.Quote(exe) + "; exit $?"}
	}
	os.Setenv("RUNNER_TEST_MODE", mode)
	e.t.Cleanup(func() { os.Unsetenv("RUNNER_TEST_MODE") })
	out, _ := os.CreateTemp(e.dir, "out")
	ch := make(chan result, 1)
	go func() {
		code, err := Run(Options{
			Hostname: host,
			Args:     args,
			Daemon:   daemon.StartOptions{StateDir: e.dir},
			Presets:  e.presets,
			Stdout:   out, Stderr: out, Notice: io.Discard,
		})
		ch <- result{code, err}
	}()
	return ch, out
}

func (e *e2e) get(host, path string) (int, string) {
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(e.proxy)+path, nil)
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (e *e2e) waitReady(host string) string {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if code, body := e.get(host, "/"); code == 200 {
			return body
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.t.Fatalf("%s never became ready; routes: %+v", host, e.srv.Routes())
	return ""
}

func (e *e2e) quitAndWait(host string, ch <-chan result) result {
	e.t.Helper()
	e.get(host, "/quit")
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		e.t.Fatal("Run did not return after the app exited")
	}
	return result{}
}

func TestRunAppHonouringPORT(t *testing.T) {
	e := newE2E(t)
	ch, _ := e.start("honour.localhost", "port", false)
	body := e.waitReady("honour.localhost")
	rt, _ := e.srv.Lookup("honour.localhost")
	if !strings.Contains(body, "mode=port port="+strconv.Itoa(rt.Port)+" env="+strconv.Itoa(rt.Port)) ||
		!strings.Contains(body, "host=127.0.0.1") ||
		!strings.Contains(body, "url=http://honour.localhost:"+strconv.Itoa(e.proxy)) {
		t.Fatalf("unexpected app response %q (route %+v)", body, rt)
	}
	r := e.quitAndWait("honour.localhost", ch)
	if r.err != nil || r.code != 7 {
		t.Fatalf("Run = %+v, want exit code 7", r)
	}
	if _, ok := e.srv.Lookup("honour.localhost"); ok {
		t.Fatal("route not removed after the app exited")
	}
}

func TestRunAppIgnoringPORTPrintsURL(t *testing.T) {
	e := newE2E(t)
	ch, out := e.start("fixed.localhost", "fixed", true) // grandchild via sh
	body := e.waitReady("fixed.localhost")
	rt, _ := e.srv.Lookup("fixed.localhost")
	if !strings.Contains(body, "mode=fixed port="+strconv.Itoa(rt.Port)) {
		t.Fatalf("routed to wrong port: %q route %+v", body, rt)
	}
	e.quitAndWait("fixed.localhost", ch)
	b, _ := os.ReadFile(out.Name())
	if !strings.Contains(string(b), "Server running at http://localhost:") {
		t.Fatalf("child output not passed through: %q", b)
	}
}

func TestRunAppFoundByProbe(t *testing.T) {
	e := newE2E(t)
	ch, _ := e.start("silent.localhost", "silent", true)
	body := e.waitReady("silent.localhost")
	if !strings.HasPrefix(body, "mode=silent") {
		t.Fatalf("got %q", body)
	}
	e.quitAndWait("silent.localhost", ch)
}

func TestRunCommandNotFound(t *testing.T) {
	e := newE2E(t)
	code, err := Run(Options{
		Hostname: "nope.localhost",
		Args:     []string{"definitely-not-a-command-aka-test"},
		Daemon:   daemon.StartOptions{StateDir: e.dir},
		Notice:   io.Discard,
	})
	if code != 127 || err == nil || !strings.Contains(err.Error(), "command not found") {
		t.Fatalf("Run = %d, %v", code, err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := e.srv.Lookup("nope.localhost"); ok {
		t.Fatal("route left behind after failed start")
	}
}

func TestRunConflict(t *testing.T) {
	e := newE2E(t)
	ch, _ := e.start("dup.localhost", "port", false)
	e.waitReady("dup.localhost")
	code, err := Run(Options{
		Hostname: "dup.localhost",
		Args:     []string{"true"},
		Daemon:   daemon.StartOptions{StateDir: e.dir},
		Notice:   io.Discard,
	})
	if code != 1 || err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Run = %d, %v; want conflict error mentioning --force", code, err)
	}
	e.quitAndWait("dup.localhost", ch)
}

func TestRunForceTakesOverAndStopsPreviousApp(t *testing.T) {
	e := newE2E(t)
	first, _ := e.start("take.localhost", "port", false)
	e.waitReady("take.localhost")
	exe, _ := os.Executable()
	second := make(chan result, 1)
	go func() {
		code, err := Run(Options{
			Hostname: "take.localhost",
			Args:     []string{exe},
			Force:    true,
			Daemon:   daemon.StartOptions{StateDir: e.dir},
			Notice:   io.Discard,
		})
		second <- result{code, err}
	}()
	select {
	case r := <-first:
		if r.err != nil {
			t.Fatalf("first run: %v", r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("first app was not stopped after being evicted")
	}
	e.waitReady("take.localhost")
	e.quitAndWait("take.localhost", second)
}

// A preset for the test binary itself: its flag sets the port the app
// listens on (it ignores PORT), and its env reaches the app.
func TestRunAppliesPreset(t *testing.T) {
	e := newE2E(t)
	f := filepath.Join(e.dir, "presets.json")
	exe, _ := os.Executable()
	os.WriteFile(f, []byte(`{"presets": [{"name": "testapp", "commands": [`+strconv.Quote(filepath.Base(exe))+`],
		"inject": [{"flags": ["--listen", "{port}"], "unless": ["--listen"]}],
		"env": {"TESTAPP_ENV": "{port}"}}]}`), 0o644)
	ps, err := presets.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	e.presets = ps
	ch, _ := e.start("preset.localhost", "flagport", false)
	body := e.waitReady("preset.localhost")
	rt, _ := e.srv.Lookup("preset.localhost")
	p := strconv.Itoa(rt.Port)
	if !strings.Contains(body, "mode=flagport port="+p+" env="+p) || !strings.Contains(body, "preset_env="+p) {
		t.Fatalf("app did not get the preset's flag and env: %q (route %+v)", body, rt)
	}
	if r := e.quitAndWait("preset.localhost", ch); r.code != 7 {
		t.Fatalf("Run = %+v", r)
	}
}
