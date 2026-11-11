package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDiagnose(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string // substring of the hint, "" for none
	}{
		{400, "<title>DisallowedHost at /</title>Invalid HTTP_HOST header: 'web.localhost:1355'.", "ALLOWED_HOSTS"},
		{403, "CSRF verification failed. Origin checking failed - https://a.localhost does not match", "SECURE_PROXY_SSL_HEADER"},
		{403, `Blocked request. This host ("web.test") is not allowed.`, "server.allowedHosts"},
		{400, "Invalid Host header", "allowedHosts"},
		{403, "<h1>Blocked hosts: web.localhost</h1>", "config.hosts"},
		{400, "Bad Request - Invalid Hostname", "AllowedHosts"},
		{400, "missing field: email", ""},
		{500, "DisallowedHost", ""}, // wrong status
		{404, "Invalid Host header", ""},
	} {
		got := Diagnose(tc.status, []byte(tc.body))
		if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%d %q: got %q, want hint containing %q", tc.status, tc.body, got, tc.want)
		}
	}
}

// The proxy reports a host rejection once, and the client still gets the
// upstream's full, unchanged response.
func TestProxyReportsHostRejection(t *testing.T) {
	h := newHarness(t)
	body := "Invalid HTTP_HOST header: 'a.localhost'. DisallowedHost" + strings.Repeat("x", 100<<10)
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, body)
	}))
	defer be.Close()
	var mu sync.Mutex
	var hints []string
	h.proxy.OnHint = func(rt Route, hint string) {
		mu.Lock()
		hints = append(hints, rt.Hostname+": "+hint)
		mu.Unlock()
	}
	h.table.set(Route{Hostname: "a.localhost", Port: backendPort(t, be)})
	for range 3 {
		resp := h.do("GET", "a.localhost", "/", nil, nil)
		if got := readBody(t, resp); resp.StatusCode != 400 || got != body {
			t.Fatalf("status %d, body changed (%d bytes, want %d)", resp.StatusCode, len(got), len(body))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hints) != 1 || !strings.Contains(hints[0], "a.localhost: Django") {
		t.Fatalf("hints = %q, want one Django hint", hints)
	}
}
