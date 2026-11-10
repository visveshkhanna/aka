package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"aka/internal/certs"
)

func startTLSDaemon(t *testing.T, cfg Config) (*Server, string) {
	t.Helper()
	cfg.StateDir = shortDir(t)
	cfg.ProxyHosts = []string{"127.0.0.1"}
	cfg.Log = log.New(io.Discard, "", 0)
	cfg.TLS = true
	s, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, cfg.StateDir
}

// routeTo registers host and points it at a backend that echoes the scheme
// it was reached through.
func routeTo(t *testing.T, dir, host string) {
	t.Helper()
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Header.Get("X-Forwarded-Proto"))
	}))
	t.Cleanup(be.Close)
	port := be.Listener.Addr().(*net.TCPAddr).Port
	c, r := register(t, dir, host, false)
	if !r.OK {
		t.Fatalf("register: %+v", r)
	}
	if r, err := c.Call(Request{Op: OpUpdate, Port: port, DialHost: "127.0.0.1"}); err != nil || !r.OK {
		t.Fatalf("update: %+v %v", r, err)
	}
}

// get fetches scheme://host:port/ through the proxy on 127.0.0.1, trusting
// the daemon's CA.
func get(t *testing.T, dir, scheme, host string, port int) string {
	t.Helper()
	ca, err := certs.LoadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots},
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get(scheme + "://" + host + ":" + strconv.Itoa(port) + "/")
	if err != nil {
		t.Fatalf("%s: %v", scheme, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestTLSDaemonServesBothSchemesOnProxyPort(t *testing.T) {
	s, dir := startTLSDaemon(t, Config{})
	r, err := Ping(dir)
	if err != nil || !r.TLS || r.HTTPSPort != 0 || r.HTTPSError != "" {
		t.Fatalf("ping = %+v %v", r, err)
	}
	routeTo(t, dir, "app.localhost")
	for _, scheme := range []string{"https", "http"} {
		if got := get(t, dir, scheme, "app.localhost", s.ProxyPort()); got != scheme {
			t.Errorf("%s: upstream saw X-Forwarded-Proto %q", scheme, got)
		}
	}
}

func TestNonTLSDaemonCreatesNoCA(t *testing.T) {
	_, dir := startDaemon(t)
	if r, _ := Ping(dir); r.TLS {
		t.Fatal("ping reports TLS for a plain daemon")
	}
	if _, err := certs.LoadCA(dir); !errors.Is(err, certs.ErrNoCA) {
		t.Fatalf("LoadCA = %v, want ErrNoCA", err)
	}
}

func TestDedicatedHTTPSPort(t *testing.T) {
	port := freePort(t)
	_, dir := startTLSDaemon(t, Config{HTTPSPort: port})
	r, _ := Ping(dir)
	if r.HTTPSPort != port || r.HTTPSError != "" || r.Privileged {
		t.Fatalf("ping = %+v", r)
	}
	routeTo(t, dir, "app.localhost")
	if got := get(t, dir, "https", "app.localhost", port); got != "https" {
		t.Fatalf("got %q", got)
	}
}

func TestHTTPSPortInUseFallsBack(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	s, dir := startTLSDaemon(t, Config{HTTPSPort: port})
	r, _ := Ping(dir)
	if r.HTTPSPort != 0 || !strings.Contains(r.HTTPSError, "in use") {
		t.Fatalf("ping = %+v", r)
	}
	routeTo(t, dir, "app.localhost")
	if got := get(t, dir, "https", "app.localhost", s.ProxyPort()); got != "https" {
		t.Fatalf("fallback port: got %q", got)
	}
}

func TestInheritedHTTPSListeners(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_, dir := startTLSDaemon(t, Config{HTTPSPort: 443, HTTPSListeners: []net.Listener{ln}})
	r, _ := Ping(dir)
	if r.HTTPSPort != port || !r.Privileged {
		t.Fatalf("ping = %+v", r)
	}
	routeTo(t, dir, "app.localhost")
	if got := get(t, dir, "https", "app.localhost", port); got != "https" {
		t.Fatalf("got %q", got)
	}

	// A privileged daemon is never restarted for a mode change: the new one
	// could not bind its ports again.
	var warned string
	r2, err := Ensure(StartOptions{StateDir: dir, TLS: false, Warn: func(s string) { warned = s }})
	if err != nil || r2.PID != r.PID {
		t.Fatalf("Ensure = %+v %v", r2, err)
	}
	if !strings.Contains(warned, "HTTPS on") {
		t.Fatalf("warning = %q", warned)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
