package cli

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/visveshkhanna/aka/internal/daemon"
)

func TestDoctor(t *testing.T) {
	dir, err := os.MkdirTemp("", "akad") // short: it holds the control socket
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("AKA_HTTPS", "0")
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"name":"shop","scripts":{"dev":"vite"}}`), 0o644)
	os.Mkdir(filepath.Join(proj, ".git"), 0o755)

	// No proxy running: only informational output, exit 0.
	var out strings.Builder
	if code := doctor(&out, dir, proj); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	for _, want := range []string{"proxy is not running", "name shop.localhost (from package.json)", "bare `aka` runs npm run dev", "preset vite adds --port PORT --strictPort --host 127.0.0.1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}

	// With a proxy, one healthy app and one Django app rejecting the host.
	s, err := daemon.Start(daemon.Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	route := func(host string, h http.HandlerFunc) {
		be := httptest.NewServer(h)
		t.Cleanup(be.Close)
		c, err := daemon.Dial(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		c.Call(daemon.Request{Op: daemon.OpRegister, Hostname: host, PID: os.Getpid(), Command: "test"})
		c.Call(daemon.Request{Op: daemon.OpUpdate, Port: be.Listener.Addr().(*net.TCPAddr).Port, DialHost: "127.0.0.1"})
	}
	route("good.localhost", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	route("dj.localhost", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, "DisallowedHost: Invalid HTTP_HOST header")
	})
	out.Reset()
	if code := doctor(&out, dir, proj); code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out.String())
	}
	for _, want := range []string{"ok    http://good.localhost:", "(app, HTTP 200)", "FAIL  http://dj.localhost:", "ALLOWED_HOSTS"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}
