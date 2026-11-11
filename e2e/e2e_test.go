//go:build e2e

// Package e2e runs the real aka binary against real dev servers (Node and
// Python) on the host OS: routing, presets, port detection, HTTPS, aliases,
// aka up, and stopping apps. It is the same test on macOS, Linux and
// Windows, and is what CI runs on each.
//
//	go test -tags e2e ./e2e -v
//
// It needs node and python on PATH. Each run uses its own state directory
// and proxy port, so it does not touch a proxy you already have running.
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var akaBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "akabin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	akaBin = filepath.Join(dir, "aka")
	if runtime.GOOS == "windows" {
		akaBin += ".exe"
	}
	if b := os.Getenv("AKA_E2E_BIN"); b != "" {
		akaBin = b // test a binary built elsewhere (the release build)
	} else {
		build := exec.Command("go", "build", "-o", akaBin, "../cmd/aka")
		build.Stdout, build.Stderr = os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "building aka:", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// server.js answers every request with JSON describing itself. With "own"
// it ignores PORT and listens on a port of its own choosing, which aka must
// find by watching the process's sockets. It prints the port without a
// URL, so output scanning cannot find it.
const serverJS = `
const http = require('http');
const own = process.argv[2] === 'own';
const name = process.argv[3] || '';
const s = http.createServer((req, res) => {
  res.setHeader('content-type', 'application/json');
  res.end(JSON.stringify({
    name, port: s.address().port, url: process.env.AKA_URL || '',
    host: req.headers.host, proto: req.headers['x-forwarded-proto'] || '',
  }));
});
s.listen(own ? 0 : Number(process.env.PORT), '127.0.0.1', () => console.log('ready ' + s.address().port));
`

type info struct {
	Name  string `json:"name"`
	Port  int    `json:"port"`
	URL   string `json:"url"`
	Host  string `json:"host"`
	Proto string `json:"proto"`
}

// harness is one isolated aka setup: state directory, proxy port, env.
type harness struct {
	t      *testing.T
	state  string
	port   int
	work   string
	env    []string
	client *http.Client
	roots  *x509.CertPool // filled by trustCA
	mu     sync.Mutex
	procs  []*proc
}

func newHarness(t *testing.T) *harness {
	// A short state path: the control socket's path must fit in sun_path.
	state, err := os.MkdirTemp("", "akae2e")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, state: state, port: freePort(t), work: t.TempDir(), roots: x509.NewCertPool()}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "AKA_") {
			h.env = append(h.env, kv)
		}
	}
	h.env = append(h.env,
		"AKA_STATE_DIR="+state,
		"AKA_PROXY_PORT="+strconv.Itoa(h.port),
		"AKA_HTTPS_PORT=0", // HTTPS shares the proxy port; never try 443
		"AKA_NO_PTY=1",
		"AKA_PREFIX=",
	)
	if err := os.WriteFile(filepath.Join(h.work, "server.js"), []byte(serverJS), 0o644); err != nil {
		t.Fatal(err)
	}
	h.client = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// Every *.localhost name goes to the proxy on loopback, the way
			// browsers resolve them, whatever the system resolver does.
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, _ := net.SplitHostPort(addr)
				var d net.Dialer
				return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
			},
			// Set up front: the transport fixes its HTTP/2 setup on first use.
			TLSClientConfig:   &tls.Config{RootCAs: h.roots},
			ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	t.Cleanup(h.cleanup)
	return h
}

func (h *harness) cleanup() {
	h.mu.Lock()
	procs := h.procs
	h.mu.Unlock()
	for _, p := range procs {
		p.kill()
	}
	out, _ := h.aka("proxy", "stop")
	h.t.Logf("aka proxy stop: %s", strings.TrimSpace(out))
	if h.t.Failed() {
		if b, err := os.ReadFile(filepath.Join(h.state, "daemon.log")); err == nil {
			h.t.Logf("daemon log:\n%s", b)
		}
	}
	os.RemoveAll(h.state)
}

// aka runs an aka command to completion.
func (h *harness) aka(args ...string) (string, error) {
	cmd := exec.Command(akaBin, args...)
	cmd.Env, cmd.Dir = h.env, h.work
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// proc is a long-running aka (an app or aka up).
type proc struct {
	name string
	cmd  *exec.Cmd
	out  *syncBuffer
	done chan struct{}
	err  error
}

func (h *harness) start(name, dir string, args ...string) *proc {
	h.t.Helper()
	cmd := exec.Command(akaBin, args...)
	cmd.Env, cmd.Dir = h.env, dir
	p := &proc{name: name, cmd: cmd, out: &syncBuffer{}, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = p.out, p.out
	setGroup(cmd)
	if err := cmd.Start(); err != nil {
		h.t.Fatalf("start %s: %v", name, err)
	}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	h.mu.Lock()
	h.procs = append(h.procs, p)
	h.mu.Unlock()
	h.t.Cleanup(func() {
		if h.t.Failed() {
			h.t.Logf("output of %s:\n%s", name, p.out.String())
		}
	})
	return p
}

func (p *proc) kill() {
	select {
	case <-p.done:
		return
	default:
	}
	p.cmd.Process.Kill()
	<-p.done
}

// stop ends an aka the way a user does: Ctrl+C on Unix. Windows has no
// signal to send to another console process, so the process is killed,
// which must still take the app down with it (job object).
func (p *proc) stop(t *testing.T) {
	t.Helper()
	interrupt(p.cmd)
	select {
	case <-p.done:
	case <-time.After(20 * time.Second):
		t.Errorf("%s did not exit after interrupt", p.name)
		p.kill()
	}
}

func (h *harness) url(scheme, host, path string) string {
	return fmt.Sprintf("%s://%s.localhost:%d%s", scheme, host, h.port, path)
}

// get fetches a URL through the proxy.
func (h *harness) get(url string) (*http.Response, []byte, error) {
	resp, err := h.client.Get(url)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp, b, err
}

// app waits until url answers 200 with JSON from server.js, and returns it.
func (h *harness) app(url string, p *proc) info {
	h.t.Helper()
	var last string
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if p != nil {
			select {
			case <-p.done:
				h.t.Fatalf("%s exited early (%v):\n%s", p.name, p.err, p.out.String())
			default:
			}
		}
		resp, body, err := h.get(url)
		if err == nil && resp.StatusCode == 200 {
			var i info
			if err := json.Unmarshal(body, &i); err != nil {
				h.t.Fatalf("%s: %v: %s", url, err, body)
			}
			return i
		}
		if err != nil {
			last = err.Error()
		} else {
			last = fmt.Sprintf("%d %s", resp.StatusCode, bytes.TrimSpace(body))
		}
		time.Sleep(250 * time.Millisecond)
	}
	h.t.Fatalf("%s never answered 200 (last: %s)", url, last)
	return info{}
}

// trustCA makes the client trust the proxy's local CA.
func (h *harness) trustCA() {
	h.t.Helper()
	pemBytes, err := os.ReadFile(filepath.Join(h.state, "ca.pem"))
	if err != nil {
		h.t.Fatal(err)
	}
	if !h.roots.AppendCertsFromPEM(pemBytes) {
		h.t.Fatal("ca.pem holds no certificate")
	}
}

func waitClosed(t *testing.T, what string, port int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			return
		}
		c.Close()
		time.Sleep(250 * time.Millisecond)
	}
	t.Errorf("%s still listening on %d: the app outlived its aka", what, port)
}

func TestApps(t *testing.T) {
	h := newHarness(t)
	node, python := need(t, "node"), pythonCmd(t)

	// PORT is honoured: the route goes to the assigned port.
	env := h.start("env", h.work, "--name", "env", node, "server.js", "port", "env")
	// No PORT support: aka finds the port the app chose.
	own := h.start("own", h.work, "--name", "own", node, "server.js", "own", "own")
	// A preset: python -m http.server takes the port as an argument.
	static := filepath.Join(h.work, "static")
	os.MkdirAll(static, 0o755)
	os.WriteFile(filepath.Join(static, "hello.txt"), []byte("hello from aka"), 0o644)
	py := h.start("static", static, "--name", "static", python, "-m", "http.server")

	var envInfo, ownInfo info
	t.Run("PORT", func(t *testing.T) {
		envInfo = h.app(h.url("http", "env", "/"), env)
		if envInfo.Name != "env" || envInfo.Host != fmt.Sprintf("env.localhost:%d", h.port) {
			t.Errorf("got %+v", envInfo)
		}
		if !strings.Contains(envInfo.URL, "env.localhost") {
			t.Errorf("AKA_URL = %q", envInfo.URL)
		}
	})
	t.Run("detection", func(t *testing.T) {
		ownInfo = h.app(h.url("http", "own", "/"), own)
		if ownInfo.Name != "own" {
			t.Errorf("got %+v", ownInfo)
		}
	})
	t.Run("preset", func(t *testing.T) {
		var body []byte
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			resp, b, err := h.get(h.url("http", "static", "/hello.txt"))
			if err == nil && resp.StatusCode == 200 {
				body = b
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if string(body) != "hello from aka" {
			t.Fatalf("hello.txt = %q\n%s", body, py.out.String())
		}
	})
	t.Run("https", func(t *testing.T) {
		h.trustCA()
		resp, body, err := h.get(h.url("https", "env", "/"))
		if err != nil {
			t.Fatal(err)
		}
		var i info
		json.Unmarshal(body, &i)
		if resp.StatusCode != 200 || i.Proto != "https" {
			t.Errorf("status %d, X-Forwarded-Proto %q", resp.StatusCode, i.Proto)
		}
		if resp.ProtoMajor != 2 {
			t.Errorf("negotiated %s, want HTTP/2", resp.Proto)
		}
	})
	t.Run("unknown host", func(t *testing.T) {
		resp, _, err := h.get(h.url("http", "nothing-here", "/"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 404 || resp.Header.Get("X-Aka") == "" {
			t.Errorf("status %d, X-Aka %q", resp.StatusCode, resp.Header.Get("X-Aka"))
		}
	})
	t.Run("list", func(t *testing.T) {
		out, err := h.aka("list", "--json")
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		var routes []struct {
			Hostname string `json:"hostname"`
			Port     int    `json:"port"`
		}
		if err := json.Unmarshal([]byte(out), &routes); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		got := map[string]int{}
		for _, r := range routes {
			got[r.Hostname] = r.Port
		}
		for _, n := range []string{"env.localhost", "own.localhost", "static.localhost"} {
			if got[n] == 0 {
				t.Errorf("%s missing from %s", n, out)
			}
		}
		if ownInfo.Port != 0 && got["own.localhost"] != ownInfo.Port {
			t.Errorf("own routed to %d, app listens on %d", got["own.localhost"], ownInfo.Port)
		}
	})
	t.Run("curl", func(t *testing.T) {
		// curl resolves *.localhost to loopback by itself, like browsers.
		curl, err := exec.LookPath("curl")
		if err != nil {
			t.Skip("no curl")
		}
		args := []string{"-sS", "--fail", "--max-time", "10"}
		if runtime.GOOS != "windows" { // Schannel curl does not take --cacert files
			args = append(args, "--cacert", filepath.Join(h.state, "ca.pem"), h.url("https", "static", "/hello.txt"))
		} else {
			args = append(args, h.url("http", "static", "/hello.txt"))
		}
		out, err := exec.Command(curl, args...).CombinedOutput()
		if err != nil || string(out) != "hello from aka" {
			t.Errorf("curl %v: %v: %s", args, err, out)
		}
	})
	t.Run("doctor", func(t *testing.T) {
		out, err := h.aka("doctor")
		t.Logf("aka doctor (%v):\n%s", err, out) // fails without a trusted CA; informational
	})
	t.Run("stop", func(t *testing.T) {
		env.stop(t)
		if envInfo.Port != 0 {
			waitClosed(t, "env app", envInfo.Port)
		}
		own.stop(t)
		if ownInfo.Port != 0 {
			waitClosed(t, "own app", ownInfo.Port)
		}
		py.stop(t)
		resp, _, err := h.get(h.url("http", "env", "/"))
		if err == nil && resp.StatusCode != 404 {
			t.Errorf("env still routed after stop: %d", resp.StatusCode)
		}
	})
}

func TestAlias(t *testing.T) {
	h := newHarness(t)
	node := need(t, "node")
	port := freePort(t)
	srv := exec.Command(node, "server.js", "port", "external")
	srv.Dir = h.work
	srv.Env = append(append([]string{}, h.env...), "PORT="+strconv.Itoa(port))
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Process.Kill(); srv.Wait() })

	if out, err := h.aka("alias", "ext", strconv.Itoa(port)); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if i := h.app(h.url("http", "ext", "/"), nil); i.Name != "external" || i.Port != port {
		t.Errorf("got %+v", i)
	}
	if out, err := h.aka("alias", "rm", "ext"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if resp, _, err := h.get(h.url("http", "ext", "/")); err != nil || resp.StatusCode != 404 {
		t.Errorf("after alias rm: %v %v", resp.StatusCode, err)
	}
}

func TestUp(t *testing.T) {
	h := newHarness(t)
	need(t, "node")
	// Plain "node": a full path may hold spaces (C:\Program Files) and the
	// command is split on whitespace.
	toml := "[apps]\nweb = 'node server.js port web'\napi = 'node server.js own api'\n"
	if err := os.WriteFile(filepath.Join(h.work, "aka.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	up := h.start("up", h.work, "up")
	web := h.app(h.url("http", "web", "/"), up)
	api := h.app(h.url("http", "api", "/"), up)
	if web.Name != "web" || api.Name != "api" {
		t.Fatalf("web %+v, api %+v", web, api)
	}
	up.stop(t)
	waitClosed(t, "web", web.Port)
	waitClosed(t, "api", api.Port)
}

// TestOrphan kills aka outright: the daemon must notice the dropped
// connection, remove the route and stop the app aka left behind.
func TestOrphan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by TestApps: on Windows every stop is a kill")
	}
	h := newHarness(t)
	node := need(t, "node")
	p := h.start("orphan", h.work, "--name", "orphan", node, "server.js", "port", "orphan")
	i := h.app(h.url("http", "orphan", "/"), p)
	p.cmd.Process.Kill() // SIGKILL: aka gets no chance to clean up
	<-p.done
	waitClosed(t, "orphaned app", i.Port)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, _, err := h.get(h.url("http", "orphan", "/"))
		if err == nil && resp.StatusCode == 404 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("route not removed after aka died")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func need(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	return p
}

// pythonCmd finds Python 3. On Windows, python3 is often the Store stub.
func pythonCmd(t *testing.T) string {
	t.Helper()
	names := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		names = []string{"python", "py"}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("python not installed")
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
