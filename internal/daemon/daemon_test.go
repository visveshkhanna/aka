package daemon

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// shortDir returns a temp dir with a short path: Unix socket paths are
// limited to ~104 bytes on macOS and t.TempDir() paths can exceed that.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "akat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func startDaemon(t *testing.T) (*Server, string) {
	t.Helper()
	dir := shortDir(t)
	s, err := Start(Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, dir
}

func register(t *testing.T, dir, host string, force bool) (*Conn, Response) {
	t.Helper()
	c, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	r, err := c.Call(Request{Op: OpRegister, Hostname: host, PID: os.Getpid(), Command: "test", Force: force})
	if err != nil {
		t.Fatal(err)
	}
	return c, r
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPing(t *testing.T) {
	s, dir := startDaemon(t)
	r, err := Ping(dir)
	if err != nil || !r.OK || r.ProxyPort != s.ProxyPort() || r.PID != os.Getpid() || r.BuildID != BuildID() {
		t.Fatalf("ping = %+v, %v", r, err)
	}
}

func TestSecondDaemonRefused(t *testing.T) {
	_, dir := startDaemon(t)
	_, err := Start(Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("err = %v, want ErrAlreadyRunning", err)
	}
}

func TestProxyPortInUseIsReported(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	_, err := Start(Config{StateDir: shortDir(t), ProxyPort: ln.Addr().(*net.TCPAddr).Port,
		ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err == nil || !strings.Contains(err.Error(), "proxy cannot listen") {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterUpdateAndRouteEndToEnd(t *testing.T) {
	s, dir := startDaemon(t)
	c, r := register(t, dir, "myapp.localhost", false)
	if !r.OK || r.Assigned < 4000 || r.Assigned > 4999 {
		t.Fatalf("register = %+v", r)
	}
	rt, ok := s.Lookup("myapp.localhost")
	if !ok || rt.Port != 0 {
		t.Fatalf("route should exist and be pending: %+v %v", rt, ok)
	}

	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "app:"+r.Host) }))
	defer be.Close()
	_, bp, _ := net.SplitHostPort(strings.TrimPrefix(be.URL, "http://"))
	port, _ := strconv.Atoi(bp)

	get := func() (int, string) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(s.ProxyPort())+"/", nil)
		req.Host = "myapp.localhost"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, _ := get(); code != 503 {
		t.Fatalf("pending route should give 503, got %d", code)
	}
	if r, err := c.Call(Request{Op: OpUpdate, Port: port, DialHost: "127.0.0.1"}); err != nil || !r.OK {
		t.Fatalf("update = %+v %v", r, err)
	}
	if code, body := get(); code != 200 || body != "app:myapp.localhost" {
		t.Fatalf("got %d %q", code, body)
	}

	// The session may still ping and list.
	if r, err := c.Call(Request{Op: OpList}); err != nil || len(r.Routes) != 1 || r.Routes[0].Port != port {
		t.Fatalf("list = %+v %v", r, err)
	}

	c.Close()
	waitFor(t, "route removal", func() bool { _, ok := s.Lookup("myapp.localhost"); return !ok })
	if code, _ := get(); code != 404 {
		t.Fatalf("after close got %d, want 404", code)
	}
}

func TestConflictWithoutForce(t *testing.T) {
	_, dir := startDaemon(t)
	register(t, dir, "a.localhost", false)
	_, r := register(t, dir, "a.localhost", false)
	if r.OK || r.Code != CodeConflict || r.OwnerPID != os.Getpid() {
		t.Fatalf("second register = %+v", r)
	}
}

func TestForceEvictsPreviousOwner(t *testing.T) {
	s, dir := startDaemon(t)
	old, r1 := register(t, dir, "a.localhost", false)
	if !r1.OK {
		t.Fatal(r1.Error)
	}
	newc, r2 := register(t, dir, "a.localhost", true)
	if !r2.OK {
		t.Fatalf("forced register = %+v", r2)
	}
	ev, err := old.Recv()
	if err != nil || ev.Event != EventEvicted {
		t.Fatalf("old session got %+v %v, want eviction", ev, err)
	}
	// The old connection closing must not remove the new owner's route.
	old.Close()
	time.Sleep(50 * time.Millisecond)
	if _, ok := s.Lookup("a.localhost"); !ok {
		t.Fatal("route vanished after evicted session closed")
	}
	if r, _ := newc.Call(Request{Op: OpUpdate, Port: 1234}); !r.OK {
		t.Fatalf("new owner update failed: %+v", r)
	}
	if rt, _ := s.Lookup("a.localhost"); rt.Port != 1234 {
		t.Fatalf("route = %+v", rt)
	}
}

func TestConcurrentRegistersGetDistinctPorts(t *testing.T) {
	_, dir := startDaemon(t)
	const n = 20
	var mu sync.Mutex
	seen := map[int]bool{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := Dial(dir)
			if err != nil {
				t.Error(err)
				return
			}
			t.Cleanup(func() { c.Close() })
			r, err := c.Call(Request{Op: OpRegister, Hostname: fmt.Sprintf("app%d.localhost", i), PID: 1})
			if err != nil || !r.OK {
				t.Errorf("register %d: %+v %v", i, r, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[r.Assigned] {
				t.Errorf("port %d assigned twice", r.Assigned)
			}
			seen[r.Assigned] = true
		}(i)
	}
	wg.Wait()
}

func TestPreferPortReused(t *testing.T) {
	_, dir := startDaemon(t)
	c, _ := Dial(dir)
	defer c.Close()
	r, _ := c.Call(Request{Op: OpRegister, Hostname: "a.localhost", PreferPort: 4321})
	if r.Assigned != 4321 {
		t.Fatalf("assigned %d, want preferred 4321", r.Assigned)
	}
	// A port already reserved by another route is not reused.
	c2, _ := Dial(dir)
	defer c2.Close()
	r2, _ := c2.Call(Request{Op: OpRegister, Hostname: "b.localhost", PreferPort: 4321})
	if !r2.OK || r2.Assigned == 4321 {
		t.Fatalf("second register = %+v", r2)
	}
}

func TestInvalidRequests(t *testing.T) {
	_, dir := startDaemon(t)
	cases := []Request{
		{Op: "bogus"},
		{Op: OpRegister},           // no hostname
		{Op: OpUpdate, Port: 1000}, // not registered
	}
	for _, req := range cases {
		r, err := Do(dir, req)
		if err != nil || r.OK || r.Error == "" {
			t.Errorf("%+v: got %+v %v, want error", req, r, err)
		}
	}
	c, _ := register(t, dir, "x.localhost", false)
	if r, _ := c.Call(Request{Op: OpUpdate, Port: 70000}); r.OK {
		t.Error("port 70000 accepted")
	}
	if r, _ := c.Call(Request{Op: OpRegister, Hostname: "y.localhost"}); r.OK {
		t.Error("double register on one session accepted")
	}
}

func TestShutdown(t *testing.T) {
	s, dir := startDaemon(t)
	register(t, dir, "a.localhost", false)
	if err := Stop(dir, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not shut down")
	}
	if _, err := os.Stat(SocketPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("socket still present: %v", err)
	}
	// The lock is released, so a new daemon can start in the same dir.
	s2, err := Start(Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	s2.Close()
}
