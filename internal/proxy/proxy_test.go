package proxy

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aka/internal/brand"
)

type memTable struct {
	mu     sync.Mutex
	routes map[string]Route
}

func (m *memTable) Lookup(h string) (Route, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.routes[h]
	return r, ok
}

func (m *memTable) Routes() []Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Route
	for _, r := range m.routes {
		out = append(out, r)
	}
	return out
}

func (m *memTable) set(r Route) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.routes == nil {
		m.routes = map[string]Route{}
	}
	m.routes[r.Hostname] = r
}

// harness starts the proxy on a real listener.
type harness struct {
	t     *testing.T
	table *memTable
	srv   *httptest.Server
	addr  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	tb := &memTable{}
	p := New(tb)
	p.Log = log.New(io.Discard, "", 0)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")
	return &harness{t: t, table: tb, srv: srv, addr: addr}
}

func (h *harness) do(method, host, path string, hdr map[string]string, body io.Reader) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(method, "http://"+h.addr+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func backendPort(t *testing.T, s *httptest.Server) int {
	t.Helper()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(s.URL, "http://"))
	n, _ := strconv.Atoi(p)
	return n
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"MyApp.Localhost:1355": "myapp.localhost",
		"myapp.localhost.":     "myapp.localhost",
		"myapp.localhost":      "myapp.localhost",
		"[::1]:80":             "::1",
		"[::1]":                "::1",
		" a.localhost ":        "a.localhost",
	}
	for in, want := range cases {
		if got := NormalizeHost(in); got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProxiesToMatchingRoutePreservingHost(t *testing.T) {
	h := newHarness(t)
	var got *http.Request
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		w.Header().Set("X-Backend", "yes")
		fmt.Fprint(w, "hello from backend")
	}))
	defer be.Close()
	h.table.set(Route{Hostname: "myapp.localhost", Port: backendPort(t, be), DialHost: "127.0.0.1"})

	resp := h.do("GET", "MyApp.localhost:1355", "/path?q=1", nil, nil)
	if resp.StatusCode != 200 || readBody(t, resp) != "hello from backend" {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Backend") != "yes" || resp.Header.Get(brand.ProxyHeader) != "1" {
		t.Fatalf("headers: %v", resp.Header)
	}
	if got.Host != "MyApp.localhost:1355" {
		t.Errorf("upstream Host = %q, want original Host", got.Host)
	}
	if got.URL.RequestURI() != "/path?q=1" {
		t.Errorf("upstream URI = %q", got.URL.RequestURI())
	}
	if got.Header.Get("X-Forwarded-Host") != "MyApp.localhost:1355" ||
		got.Header.Get("X-Forwarded-Proto") != "http" ||
		got.Header.Get("X-Forwarded-For") == "" ||
		got.Header.Get("X-Forwarded-Port") != "1355" {
		t.Errorf("forwarded headers: %v", got.Header)
	}
	if got.Header.Get(brand.HopsHeader) != "1" {
		t.Errorf("hops = %q, want 1", got.Header.Get(brand.HopsHeader))
	}
}

func TestUnknownDialHostTriesIPv4ThenIPv6(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			addr := "127.0.0.1:0"
			if network == "tcp6" {
				addr = "[::1]:0"
			}
			ln, err := net.Listen(network, addr)
			if err != nil {
				t.Skipf("%s unavailable: %v", network, err)
			}
			be := &httptest.Server{Listener: ln, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, network)
			})}}
			be.Start()
			defer be.Close()
			h := newHarness(t)
			h.table.set(Route{Hostname: "v.localhost", Port: ln.Addr().(*net.TCPAddr).Port}) // no DialHost
			resp := h.do("GET", "v.localhost", "/", nil, nil)
			if body := readBody(t, resp); resp.StatusCode != 200 || body != network {
				t.Fatalf("status %d body %q", resp.StatusCode, body)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	h := newHarness(t)
	h.table.set(Route{Hostname: "api.localhost", Port: 4001, PID: 7, Since: time.Now().Add(-2 * time.Hour)})
	h.table.set(Route{Hostname: "db.localhost", Port: 5432})

	resp := h.do("GET", "nope.localhost", "/", nil, nil)
	if resp.StatusCode != 404 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d ct %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var j map[string]string
	json.NewDecoder(resp.Body).Decode(&j)
	if j["error"] != "not_found" || j["host"] != "nope.localhost" {
		t.Fatalf("json %v", j)
	}
	if resp.Header.Get(brand.ProxyHeader) != "1" {
		t.Fatal("missing proxy header on 404")
	}

	resp = h.do("GET", "nope.localhost:"+strconv.Itoa(h.srvPort()), "/", map[string]string{"Accept": "text/html,*/*"}, nil)
	body := readBody(t, resp)
	if resp.StatusCode != 404 || !strings.Contains(body, "No app at nope.localhost") {
		t.Fatalf("html 404: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "aka nope &lt;command&gt;") {
		t.Errorf("missing start hint: %s", body)
	}
	link := fmt.Sprintf(`href="http://api.localhost:%d"`, h.srvPort())
	if !strings.Contains(body, link) || !strings.Contains(body, "up 2h 0m") || !strings.Contains(body, "alias") {
		t.Errorf("route list missing link %s, uptime or alias label:\n%s", link, body)
	}
}

func (h *harness) srvPort() int {
	_, p, _ := net.SplitHostPort(h.addr)
	n, _ := strconv.Atoi(p)
	return n
}

func TestNotFoundEscapesHostnames(t *testing.T) {
	h := newHarness(t)
	h.table.set(Route{Hostname: "<img src=x onerror=alert(1)>.localhost", Port: 1})
	resp := h.do("GET", "<script>alert(1)</script>.localhost", "/", map[string]string{"Accept": "text/html"}, nil)
	body := readBody(t, resp)
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
		t.Fatalf("unescaped hostname in page:\n%s", body)
	}
}

func TestMissingHost(t *testing.T) {
	h := newHarness(t)
	c, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "GET / HTTP/1.0\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 || resp.Header.Get(brand.ProxyHeader) != "1" {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestPendingRouteReturns503WithRefresh(t *testing.T) {
	h := newHarness(t)
	h.table.set(Route{Hostname: "slow.localhost", Port: 0, PID: 1})
	resp := h.do("GET", "slow.localhost", "/", map[string]string{"Accept": "text/html"}, nil)
	body := readBody(t, resp)
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "1" || !strings.Contains(body, `http-equiv="refresh"`) {
		t.Fatalf("status %d retry %q body %s", resp.StatusCode, resp.Header.Get("Retry-After"), body)
	}
}

func TestRefusedConnectionShowsRestartingPage(t *testing.T) {
	h := newHarness(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	h.table.set(Route{Hostname: "dead.localhost", Port: port, DialHost: "127.0.0.1"})
	resp := h.do("GET", "dead.localhost", "/", nil, nil)
	var j map[string]string
	json.NewDecoder(resp.Body).Decode(&j)
	if resp.StatusCode != 503 || j["error"] != "unavailable" || !strings.Contains(j["message"], "Nothing is listening") {
		t.Fatalf("status %d json %v", resp.StatusCode, j)
	}
	resp = h.do("GET", "dead.localhost", "/", map[string]string{"Accept": "text/html"}, nil)
	body := readBody(t, resp)
	if !strings.Contains(body, "not accepting connections") || !strings.Contains(body, `http-equiv="refresh"`) {
		t.Fatalf("html: %s", body)
	}
}

func TestBadGatewayWhenBackendHangsUp(t *testing.T) {
	h := newHarness(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close() // accept, then close without answering
		}
	}()
	h.table.set(Route{Hostname: "rude.localhost", Port: ln.Addr().(*net.TCPAddr).Port, DialHost: "127.0.0.1"})
	resp := h.do("GET", "rude.localhost", "/", map[string]string{"Accept": "text/html"}, nil)
	if body := readBody(t, resp); resp.StatusCode != 502 || !strings.Contains(body, "rude.localhost is not responding") {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
}

func TestUpstreamErrorsPassThroughUnchanged(t *testing.T) {
	h := newHarness(t)
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		fmt.Fprint(w, `{"detail":"boom"}`)
	}))
	defer be.Close()
	h.table.set(Route{Hostname: "e.localhost", Port: backendPort(t, be)})
	resp := h.do("GET", "e.localhost", "/", map[string]string{"Accept": "text/html"}, nil)
	if body := readBody(t, resp); resp.StatusCode != 500 || body != `{"detail":"boom"}` {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestLoopDetection(t *testing.T) {
	h := newHarness(t)
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer be.Close()
	h.table.set(Route{Hostname: "a.localhost", Port: backendPort(t, be)})

	resp := h.do("GET", "a.localhost", "/", map[string]string{brand.HopsHeader: strconv.Itoa(MaxHops - 1)}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("below threshold: %d", resp.StatusCode)
	}
	resp = h.do("GET", "a.localhost", "/", map[string]string{brand.HopsHeader: strconv.Itoa(MaxHops)}, nil)
	if resp.StatusCode != http.StatusLoopDetected {
		t.Fatalf("at threshold: %d", resp.StatusCode)
	}
}

// TestRealLoop routes an app to a backend that forwards every request back
// through the proxy with the same Host, as a misconfigured dev-server proxy
// would. The hop counter must stop it.
func TestRealLoop(t *testing.T) {
	h := newHarness(t)
	var calls int
	var mu sync.Mutex
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		req, _ := http.NewRequest("GET", "http://"+h.addr+r.URL.Path, nil)
		req.Host = r.Host
		req.Header.Set(brand.HopsHeader, r.Header.Get(brand.HopsHeader))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer be.Close()
	h.table.set(Route{Hostname: "loop.localhost", Port: backendPort(t, be)})
	resp := h.do("GET", "loop.localhost", "/", nil, nil)
	if resp.StatusCode != http.StatusLoopDetected {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if calls != MaxHops {
		t.Fatalf("backend called %d times, want %d", calls, MaxHops)
	}
}

func TestDoesNotForwardHopByHopHeaders(t *testing.T) {
	h := newHarness(t)
	var got http.Header
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer be.Close()
	h.table.set(Route{Hostname: "a.localhost", Port: backendPort(t, be)})
	h.do("GET", "a.localhost", "/", map[string]string{
		"Connection": "keep-alive, X-Secret", "X-Secret": "1", "Proxy-Connection": "keep-alive", "Keep-Alive": "timeout=5",
	}, nil)
	for _, k := range []string{"X-Secret", "Proxy-Connection", "Keep-Alive"} {
		if got.Get(k) != "" {
			t.Errorf("hop-by-hop header %s forwarded", k)
		}
	}
}

func TestStreamsChunkedRequestBody(t *testing.T) {
	h := newHarness(t)
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s %d %v", r.Method, len(b), r.TransferEncoding)
	}))
	defer be.Close()
	h.table.set(Route{Hostname: "a.localhost", Port: backendPort(t, be)})
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 100; i++ {
			pw.Write([]byte(strings.Repeat("x", 1000)))
		}
		pw.Close()
	}()
	resp := h.do("DELETE", "a.localhost", "/", nil, pr)
	if body := readBody(t, resp); body != "DELETE 100000 [chunked]" {
		t.Fatalf("body %q", body)
	}
}

func TestSurvivesBackendClosingKeptAliveConnections(t *testing.T) {
	h := newHarness(t)
	be := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	be.Start()
	port := backendPort(t, be)
	h.table.set(Route{Hostname: "a.localhost", Port: port, DialHost: "127.0.0.1"})
	for i := 0; i < 3; i++ {
		if resp := h.do("GET", "a.localhost", "/", nil, nil); resp.StatusCode != 200 {
			t.Fatalf("warmup %d", resp.StatusCode)
		}
	}
	be.CloseClientConnections() // kill pooled upstream sockets
	for i := 0; i < 3; i++ {
		if resp := h.do("GET", "a.localhost", "/", nil, nil); resp.StatusCode != 200 {
			t.Fatalf("after reset: %d", resp.StatusCode)
		}
	}
	be.Close()
}

func TestRewriteLocation(t *testing.T) {
	cases := []struct {
		loc  string
		want string
		ok   bool
	}{
		{"http://localhost:4123/login?next=/a#f", "http://myapp.localhost:1355/login?next=/a#f", true},
		{"http://127.0.0.1:4123/", "http://myapp.localhost:1355/", true},
		{"http://[::1]:4123/x", "http://myapp.localhost:1355/x", true},
		{"http://0.0.0.0:4123/x", "http://myapp.localhost:1355/x", true},
		{"https://localhost:4123/x", "http://myapp.localhost:1355/x", true},
		{"http://localhost:9999/", "", false},         // other port
		{"http://example.com:4123/", "", false},       // other host
		{"/relative", "", false},                      // relative
		{"http://user:pw@localhost:4123/", "", false}, // credentials
		{"ftp://localhost:4123/", "", false},          // scheme
		{"http://localhost/", "", false},              // no port
		{"://bad", "", false},
	}
	for _, c := range cases {
		got, ok := RewriteLocation(c.loc, 4123, "http", "myapp.localhost:1355")
		if ok != c.ok || got != c.want {
			t.Errorf("RewriteLocation(%q) = %q,%v want %q,%v", c.loc, got, ok, c.want, c.ok)
		}
	}
}

func TestRewritesRedirectToRawPort(t *testing.T) {
	h := newHarness(t)
	var port int
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/raw":
			http.Redirect(w, r, fmt.Sprintf("http://localhost:%d/login", port), http.StatusFound)
		case "/other":
			http.Redirect(w, r, "https://accounts.example.com/oauth", http.StatusFound)
		default:
			http.Redirect(w, r, "/relative", http.StatusFound)
		}
	}))
	defer be.Close()
	port = backendPort(t, be)
	h.table.set(Route{Hostname: "a.localhost", Port: port})
	host := "a.localhost:" + strconv.Itoa(h.srvPort())
	cases := map[string]string{
		"/raw":   "http://" + host + "/login",
		"/other": "https://accounts.example.com/oauth",
		"/":      "/relative",
	}
	for path, want := range cases {
		if got := h.do("GET", host, path, nil, nil).Header.Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q", path, got, want)
		}
	}
}

func TestStreamsResponsesWithoutBuffering(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
	}))
	defer be.Close()
	defer close(release)
	h.table.set(Route{Hostname: "sse.localhost", Port: backendPort(t, be)})
	resp := h.do("GET", "sse.localhost", "/", nil, nil)
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- s
	}()
	select {
	case s := <-line:
		if s != "data: first\n" {
			t.Fatalf("got %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first SSE event was buffered by the proxy")
	}
}

// wsBackend answers a WebSocket handshake then echoes raw bytes, enough to
// verify the proxy hands the upgraded connection through.
func wsBackend(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !headerHasToken(r.Header, "Connection", "upgrade") || r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "not a websocket", 400)
			return
		}
		sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nX-Custom: kept\r\n\r\n",
			base64.StdEncoding.EncodeToString(sum[:]))
		rw.Flush()
		io.Copy(c, rw) // echo
	}))
}

func TestWebSocketUpgrade(t *testing.T) {
	h := newHarness(t)
	be := wsBackend(t)
	defer be.Close()
	h.table.set(Route{Hostname: "ws.localhost", Port: backendPort(t, be)})

	c, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	fmt.Fprintf(c, "GET /hmr HTTP/1.1\r\nHost: ws.localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", key)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 || resp.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" || resp.Header.Get("X-Custom") != "kept" {
		t.Fatalf("handshake: %d %v", resp.StatusCode, resp.Header)
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprint(c, "ping-bytes")
	buf := make([]byte, len("ping-bytes"))
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "ping-bytes" {
		t.Fatalf("echo %q %v", buf, err)
	}
}

func TestWebSocketUnknownHostClosesConnection(t *testing.T) {
	h := newHarness(t)
	c, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: nope.localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(make([]byte, 1))
	if n != 0 || err == nil {
		t.Fatalf("expected closed connection, got n=%d err=%v", n, err)
	}
}

func TestWebSocketLoopClosesConnection(t *testing.T) {
	h := newHarness(t)
	be := wsBackend(t)
	defer be.Close()
	h.table.set(Route{Hostname: "ws.localhost", Port: backendPort(t, be)})
	c, _ := net.Dial("tcp", h.addr)
	defer c.Close()
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: ws.localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n%s: %d\r\n\r\n", brand.HopsHeader, MaxHops)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, err := c.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("expected closed connection, got n=%d err=%v", n, err)
	}
}

func TestRouteChangeTakesEffectImmediately(t *testing.T) {
	h := newHarness(t)
	mk := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, name) }))
	}
	a, b := mk("a"), mk("b")
	defer a.Close()
	defer b.Close()
	h.table.set(Route{Hostname: "x.localhost", Port: backendPort(t, a)})
	if body := readBody(t, h.do("GET", "x.localhost", "/", nil, nil)); body != "a" {
		t.Fatalf("got %q", body)
	}
	h.table.set(Route{Hostname: "x.localhost", Port: backendPort(t, b)})
	if body := readBody(t, h.do("GET", "x.localhost", "/", nil, nil)); body != "b" {
		t.Fatalf("after switch got %q", body)
	}
}

func TestFormatUptime(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Second:             "5s",
		3 * time.Minute:             "3m",
		2*time.Hour + 5*time.Minute: "2h 5m",
		50 * time.Hour:              "2d",
	}
	for d, want := range cases {
		if got := formatUptime(d); got != want {
			t.Errorf("formatUptime(%v) = %q, want %q", d, got, want)
		}
	}
}
