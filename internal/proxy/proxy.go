// Package proxy is the Host-header reverse proxy that maps
// <name>.localhost to the port an app is listening on.
package proxy

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/netx"
)

// Route maps a hostname to a local upstream port.
type Route struct {
	Hostname string `json:"hostname"`
	// Port is the upstream port. 0 means the app is registered but has not
	// bound a port yet; the proxy answers 503 with an auto-refreshing page.
	Port int `json:"port"`
	// DialHost is the address the app bound ("127.0.0.1", "::1"). Empty
	// means unknown or wildcard: the proxy tries 127.0.0.1, then ::1.
	DialHost string `json:"dialHost,omitempty"`
	// PID is the aka process that owns the route (0 for a static alias).
	PID     int       `json:"pid"`
	Command string    `json:"command,omitempty"`
	Since   time.Time `json:"since"`
}

// Table is the live route set. Lookup is called on every request.
type Table interface {
	Lookup(hostname string) (Route, bool)
	Routes() []Route
}

// MaxHops is how many times a request may pass through the proxy before it is
// treated as a loop. Two is normal (a frontend dev server proxying API calls
// to another aka-managed backend); five leaves headroom.
const MaxHops = 5

// loopbackAlias is the upstream host used when the bind address is unknown;
// the dialer expands it to 127.0.0.1 then ::1. It never reaches DNS.
const loopbackAlias = "aka-loopback"

// Server is an http.Handler that proxies by Host header.
type Server struct {
	Table Table
	// TLD is the suffix used in help text on error pages.
	TLD string
	Log *log.Logger
	// OnHint, if set, is called once per route and hint when an upstream
	// response looks like the framework rejecting aka's Host or Origin.
	OnHint func(rt Route, hint string)

	hintMu sync.Mutex
	hinted map[string]bool

	rp *httputil.ReverseProxy
}

type ctxKey struct{}

// New returns a proxy handler over table. The same handler serves HTTP and
// HTTPS listeners; links and forwarded headers follow the request's scheme.
func New(table Table) *Server {
	s := &Server{Table: table, TLD: brand.DefaultTLD, Log: log.Default()}
	s.rp = &httputil.ReverseProxy{
		Rewrite:        s.rewrite,
		Transport:      newTransport(),
		FlushInterval:  -1, // dev servers stream (SSE, HMR); never buffer
		ModifyResponse: s.modifyResponse,
		ErrorHandler:   s.proxyError,
		ErrorLog:       log.New(discard{}, "", 0),
	}
	return s
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func newTransport() *http.Transport {
	d := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: nil, // never send local traffic through HTTP_PROXY
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil || host != loopbackAlias {
				return d.DialContext(ctx, network, addr)
			}
			// Unknown bind address: dev servers bind 127.0.0.1, ::1 or both.
			// Refusal on loopback is immediate, so trying in turn is cheap.
			c, err4 := d.DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", port))
			if err4 == nil {
				return c, nil
			}
			c, err6 := d.DialContext(ctx, "tcp6", net.JoinHostPort("::1", port))
			if err6 == nil {
				return c, nil
			}
			return nil, err4
		},
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 0, // long-polling and slow first compiles are normal in dev
		ForceAttemptHTTP2:     false,
	}
}

// NormalizeHost lowercases a Host header value and strips the port and any
// trailing dot, giving the key used for route lookup.
func NormalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	} else if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	return strings.TrimSuffix(h, ".")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(brand.ProxyHeader, "1")
	if r.Host == "" {
		s.sendError(w, r, http.StatusBadRequest, "missing_host", "Request has no Host header.", "", pageMissingHost)
		return
	}
	hops, _ := strconv.Atoi(r.Header.Get(brand.HopsHeader))
	host := NormalizeHost(r.Host)
	if hops >= MaxHops {
		s.Log.Printf("loop detected for %s (%d hops)", host, hops)
		if isUpgrade(r) {
			// No useful page can be shown for a WebSocket; just close it.
			closeConn(w)
			return
		}
		s.sendError(w, r, http.StatusLoopDetected, "loop_detected",
			"Request passed through the proxy "+strconv.Itoa(hops)+" times. A dev server is probably proxying back to itself without rewriting the Host header.",
			host, pageLoop)
		return
	}
	route, ok := s.Table.Lookup(host)
	if !ok {
		if isUpgrade(r) {
			closeConn(w)
			return
		}
		s.sendError(w, r, http.StatusNotFound, "not_found", "No app is registered for "+host+".", host, pageNotFound)
		return
	}
	if route.Port == 0 {
		w.Header().Set("Retry-After", "1")
		s.sendError(w, r, http.StatusServiceUnavailable, "starting",
			"The app is starting and has not opened a port yet.", host, pageStarting)
		return
	}
	r.Header.Set(brand.HopsHeader, strconv.Itoa(hops+1))
	s.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, route)))
}

func routeFrom(r *http.Request) Route {
	rt, _ := r.Context().Value(ctxKey{}).(Route)
	return rt
}

func upstreamHost(rt Route) string {
	h := rt.DialHost
	if h == "" {
		h = loopbackAlias
	}
	return net.JoinHostPort(h, strconv.Itoa(rt.Port))
}

func (s *Server) rewrite(pr *httputil.ProxyRequest) {
	rt := routeFrom(pr.In)
	pr.Out.URL.Scheme = "http"
	pr.Out.URL.Host = upstreamHost(rt)
	// Keep the original Host (myapp.localhost:1355): apps build absolute
	// URLs and check allowed hosts from it. SetURL would replace it.
	pr.Out.Host = pr.In.Host
	pr.SetXForwarded() // X-Forwarded-Proto is https when pr.In.TLS is set
	pr.Out.Header.Set("X-Forwarded-Port", requestPort(pr.In))
}

// requestScheme is the scheme the client used to reach the proxy.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// requestPort is the port the client used: the one in Host, or the
// scheme's default when Host has none.
func requestPort(r *http.Request) string {
	if _, p, err := net.SplitHostPort(r.Host); err == nil && p != "" {
		return p
	}
	if r.TLS != nil {
		return "443"
	}
	return "80"
}

// modifyResponse rewrites redirects that point at the app's raw port
// (http://localhost:4123/login) back to the public URL, so a redirect does
// not escape the proxy.
func (s *Server) modifyResponse(resp *http.Response) error {
	rt := routeFrom(resp.Request)
	if s.OnHint != nil {
		if hint := sniffRejection(resp); hint != "" {
			s.hint(rt, hint)
		}
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		if rewritten, ok := RewriteLocation(loc, rt.Port, requestScheme(resp.Request), resp.Request.Host); ok {
			resp.Header.Set("Location", rewritten)
		}
	}
	return nil
}

func (s *Server) hint(rt Route, hint string) {
	key := rt.Hostname + "\x00" + hint
	s.hintMu.Lock()
	if s.hinted == nil {
		s.hinted = map[string]bool{}
	}
	seen := s.hinted[key]
	s.hinted[key] = true
	s.hintMu.Unlock()
	if !seen {
		s.Log.Printf("%s: %s", rt.Hostname, hint)
		s.OnHint(rt, hint)
	}
}

// RewriteLocation rewrites an absolute Location URL that targets a loopback
// or wildcard address on upstreamPort to scheme://publicHost, keeping path,
// query and fragment. Relative URLs, other ports, other hosts and URLs with
// credentials are left alone.
func RewriteLocation(loc string, upstreamPort int, scheme, publicHost string) (string, bool) {
	u, err := url.Parse(loc)
	if err != nil || u.Host == "" || u.User != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if u.Port() != strconv.Itoa(upstreamPort) {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0", "::":
	default:
		return "", false
	}
	u.Scheme = scheme
	u.Host = publicHost
	return u.String(), true
}

func (s *Server) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return // client went away
	}
	rt := routeFrom(r)
	host := NormalizeHost(r.Host)
	s.Log.Printf("%s -> %d: %v", host, rt.Port, err)
	if netx.IsRefused(err) {
		// Usually a reloader between workers (Django, uvicorn --reload,
		// nodemon). Show the auto-refreshing page rather than a hard error;
		// the route disappears on its own if the app really exited.
		w.Header().Set("Retry-After", "1")
		s.sendError(w, r, http.StatusServiceUnavailable, "unavailable",
			"Nothing is listening on port "+strconv.Itoa(rt.Port)+". The app may be restarting.", host, pageRestarting)
		return
	}
	s.sendError(w, r, http.StatusBadGateway, "bad_gateway",
		"Could not reach the app on port "+strconv.Itoa(rt.Port)+": "+err.Error(), host, pageBadGateway)
}

func isUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") != "" && headerHasToken(r.Header, "Connection", "upgrade")
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

func closeConn(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if c, _, err := hj.Hijack(); err == nil {
			c.Close()
			return
		}
	}
	w.WriteHeader(http.StatusBadRequest)
}
