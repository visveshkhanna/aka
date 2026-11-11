package daemon

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAliasRoutesPersistsAndRemoves(t *testing.T) {
	s, dir := startDaemon(t)
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "db-admin") }))
	defer be.Close()
	port := be.Listener.Addr().(*net.TCPAddr).Port

	if r, err := Do(dir, Request{Op: OpAlias, Hostname: "db.localhost", Port: port}); err != nil || !r.OK {
		t.Fatalf("alias: %+v %v", r, err)
	}
	get := func(srv *Server) (int, string) {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(srv.ProxyPort())+"/", nil)
		req.Host = "db.localhost"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get(s); code != 200 || body != "db-admin" {
		t.Fatalf("via alias: %d %q", code, body)
	}

	// Survives a daemon restart.
	s.Close()
	s2, err := Start(Config{StateDir: dir, ProxyHosts: []string{"127.0.0.1"}, Log: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if code, _ := get(s2); code != 200 {
		t.Fatalf("alias lost on restart: %d", code)
	}
	if rt, _ := s2.Lookup("db.localhost"); rt.PID != 0 || rt.Command != "alias" {
		t.Fatalf("route %+v", rt)
	}

	if r, _ := Do(dir, Request{Op: OpAlias, Hostname: "db.localhost", Remove: true}); !r.OK {
		t.Fatalf("remove: %+v", r)
	}
	if code, _ := get(s2); code != 404 {
		t.Fatalf("after remove: %d", code)
	}
	if r, _ := Do(dir, Request{Op: OpAlias, Hostname: "db.localhost", Remove: true}); r.OK {
		t.Fatal("removing a missing alias succeeded")
	}
}

func TestAliasConflicts(t *testing.T) {
	_, dir := startDaemon(t)
	register(t, dir, "app.localhost", false)
	if r, _ := Do(dir, Request{Op: OpAlias, Hostname: "app.localhost", Port: 5432}); r.OK || r.Code != CodeConflict {
		t.Fatalf("alias over a running app: %+v", r)
	}
	if r, _ := Do(dir, Request{Op: OpAlias, Hostname: "db.localhost", Port: 0}); r.OK || r.Code != CodeInvalid {
		t.Fatalf("port 0: %+v", r)
	}
	Do(dir, Request{Op: OpAlias, Hostname: "db.localhost", Port: 5432})
	// Replacing an alias is fine.
	if r, _ := Do(dir, Request{Op: OpAlias, Hostname: "db.localhost", Port: 5433}); !r.OK {
		t.Fatalf("replace alias: %+v", r)
	}
	// An app cannot take an alias's name, not even with --force.
	for _, force := range []bool{false, true} {
		_, r := register(t, dir, "db.localhost", force)
		if r.OK || r.Code != CodeConflict || !strings.Contains(r.Error, "alias rm db") {
			t.Fatalf("register over alias (force=%v): %+v", force, r)
		}
	}
}
