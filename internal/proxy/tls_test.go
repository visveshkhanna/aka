package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/visveshkhanna/aka/internal/certs"
)

// tlsHarness serves the proxy on one port that speaks both HTTP and HTTPS,
// the way the daemon runs it.
type tlsHarness struct {
	t     *testing.T
	table *memTable
	addr  string
	port  int
	roots *x509.CertPool
}

func newTLSHarness(t *testing.T) *tlsHarness {
	t.Helper()
	ca, _, err := certs.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	is, err := certs.NewIssuer(ca)
	if err != nil {
		t.Fatal(err)
	}
	tb := &memTable{}
	p := New(tb)
	p.Log = log.New(io.Discard, "", 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	plain, tlsRaw := SplitTLS(ln)
	srv := &http.Server{Handler: p, TLSConfig: is.TLSConfig()}
	go srv.Serve(plain)
	go srv.ServeTLS(tlsRaw, "", "")
	t.Cleanup(func() { srv.Close() })
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	return &tlsHarness{t: t, table: tb, addr: ln.Addr().String(), port: ln.Addr().(*net.TCPAddr).Port, roots: roots}
}

// client returns an HTTP client that sends every request to the harness,
// whatever the URL's host, and trusts the test CA.
func (h *tlsHarness) client(h2 bool) *http.Client {
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: h.roots},
		ForceAttemptHTTP2: h2,
	}
	tr.DialContext = (&dialTo{addr: h.addr}).DialContext
	if !h2 {
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (h *tlsHarness) url(scheme, host, path string) string {
	return scheme + "://" + host + ":" + strconv.Itoa(h.port) + path
}

func TestHTTPSAndHTTPOnOnePort(t *testing.T) {
	h := newTLSHarness(t)
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"host":  r.Host,
			"proto": r.Header.Get("X-Forwarded-Proto"),
			"port":  r.Header.Get("X-Forwarded-Port"),
		})
	}))
	defer be.Close()
	h.table.set(Route{Hostname: "app.localhost", Port: backendPort(t, be)})

	for _, tc := range []struct {
		scheme string
		h2     bool
		proto  string
	}{
		{"https", true, "HTTP/2.0"},
		{"https", false, "HTTP/1.1"},
		{"http", false, "HTTP/1.1"},
	} {
		resp, err := h.client(tc.h2).Get(h.url(tc.scheme, "app.localhost", "/"))
		if err != nil {
			t.Fatalf("%s h2=%v: %v", tc.scheme, tc.h2, err)
		}
		var got map[string]string
		json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if resp.Proto != tc.proto {
			t.Errorf("%s h2=%v: proto %s, want %s", tc.scheme, tc.h2, resp.Proto, tc.proto)
		}
		want := map[string]string{"host": "app.localhost:" + strconv.Itoa(h.port), "proto": tc.scheme, "port": strconv.Itoa(h.port)}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s h2=%v: upstream saw %s=%q, want %q", tc.scheme, tc.h2, k, got[k], v)
			}
		}
	}
}

func TestHTTPSCertificateMatchesHost(t *testing.T) {
	h := newTLSHarness(t)
	for _, host := range []string{"app.localhost", "api.app.localhost"} {
		c, err := tls.Dial("tcp", h.addr, &tls.Config{ServerName: host, RootCAs: h.roots})
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if got := c.ConnectionState().PeerCertificates[0].DNSNames; len(got) != 1 || got[0] != host {
			t.Errorf("%s: certificate names %v", host, got)
		}
		c.Close()
	}
}

func TestHTTPSRedirectRewrittenToHTTPS(t *testing.T) {
	h := newTLSHarness(t)
	var port int
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%d/login", port), http.StatusFound)
	}))
	defer be.Close()
	port = backendPort(t, be)
	h.table.set(Route{Hostname: "app.localhost", Port: port})
	resp, err := h.client(true).Get(h.url("https", "app.localhost", "/"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if want := h.url("https", "app.localhost", "/login"); resp.Header.Get("Location") != want {
		t.Fatalf("Location = %q, want %q", resp.Header.Get("Location"), want)
	}
}

func TestHTTPSNotFoundLinksUseHTTPS(t *testing.T) {
	h := newTLSHarness(t)
	h.table.set(Route{Hostname: "api.localhost", Port: 4001})
	req, _ := http.NewRequest("GET", h.url("https", "nope.localhost", "/"), nil)
	req.Header.Set("Accept", "text/html")
	resp, err := h.client(true).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if link := `href="` + h.url("https", "api.localhost", "") + `"`; !strings.Contains(string(body), link) {
		t.Fatalf("missing %s in:\n%s", link, body)
	}
}

func TestWebSocketOverTLS(t *testing.T) {
	h := newTLSHarness(t)
	be := wsBackend(t)
	defer be.Close()
	h.table.set(Route{Hostname: "ws.localhost", Port: backendPort(t, be)})

	// Browsers open WebSockets over HTTP/1.1, since the server does not
	// offer extended CONNECT on HTTP/2.
	c, err := tls.Dial("tcp", h.addr, &tls.Config{ServerName: "ws.localhost", RootCAs: h.roots, NextProtos: []string{"http/1.1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "GET /hmr HTTP/1.1\r\nHost: ws.localhost:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", h.port)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("handshake status %d", resp.StatusCode)
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	io.WriteString(c, "over-tls")
	buf := make([]byte, len("over-tls"))
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "over-tls" {
		t.Fatalf("echo %q %v", buf, err)
	}
}

// A connection that sends nothing must not hold up the listener.
func TestSplitIdleConnectionDoesNotBlock(t *testing.T) {
	h := newTLSHarness(t)
	idle, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	h.table.set(Route{Hostname: "app.localhost", Port: 1})
	done := make(chan error, 1)
	go func() {
		resp, err := h.client(false).Get(h.url("http", "nope.localhost", "/"))
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request blocked behind an idle connection")
	}
}

func TestSplitCloseStopsBothHalves(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	plain, tlsRaw := SplitTLS(ln)
	errs := make(chan error, 2)
	go func() { _, err := plain.Accept(); errs <- err }()
	go func() { _, err := tlsRaw.Accept(); errs <- err }()
	tlsRaw.Close()
	for range 2 {
		select {
		case err := <-errs:
			if err == nil {
				t.Fatal("Accept succeeded after Close")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Accept did not return after Close")
		}
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("underlying listener still accepting")
	}
}

// dialTo sends every connection to one address, so URLs can carry any host.
type dialTo struct{ addr string }

func (d *dialTo) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, network, d.addr)
}
