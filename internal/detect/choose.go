package detect

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// NonHTTPPorts are well-known debugger and live-reload ports that a dev
// server's tree often opens next to the app port. They are never chosen by
// the HTTP probe fallback (they can still be chosen when they are the
// assigned port or the printed URL, since that is an explicit signal).
var NonHTTPPorts = map[int]string{
	9229:  "Node inspector",
	9230:  "Node inspector",
	5678:  "debugpy",
	35729: "LiveReload",
	5005:  "JDWP debugger",
}

// Reason explains why a port was chosen.
type Reason string

const (
	ReasonAssigned Reason = "assigned PORT"
	ReasonPrinted  Reason = "printed URL"
	ReasonCurrent  Reason = "already routed"
	ReasonProbed   Reason = "answered HTTP"
	ReasonPinned   Reason = "pinned"
)

// Choice is the chooser's verdict.
type Choice struct {
	Listener Listener
	Reason   Reason
}

// Input is everything the chooser looks at.
type Input struct {
	Assigned  int        // port we assigned via PORT (0 = none)
	Pinned    int        // user-pinned port (--port-hint); 0 = none
	Listeners []Listener // LISTEN sockets of the child tree
	Hints     []int      // printed ports, most recent first
	Current   int        // port currently routed (0 = none)
	// Probe reports whether a listener answers HTTP. May be nil, in which
	// case the probe tier is skipped.
	Probe func(Listener) bool
}

// Choose picks the upstream port, in order of reliability:
//
//  1. a pinned port (if listening)
//  2. the assigned port P (the app honoured PORT or an injected flag)
//  3. the most recently printed local URL that is listening
//  4. the currently routed port, if still listening (avoids flapping)
//  5. the first listener, by port, that answers HTTP, skipping known
//     debugger/live-reload ports
//
// It returns false when nothing qualifies yet.
func Choose(in Input) (Choice, bool) {
	byPort := make(map[int]Listener, len(in.Listeners))
	for _, l := range in.Listeners {
		if _, dup := byPort[l.Port]; !dup {
			byPort[l.Port] = l
		}
	}
	if in.Pinned > 0 {
		if l, ok := byPort[in.Pinned]; ok {
			return Choice{l, ReasonPinned}, true
		}
		// A pinned port overrides heuristics entirely: wait for it.
		return Choice{}, false
	}
	if l, ok := byPort[in.Assigned]; ok && in.Assigned > 0 {
		return Choice{l, ReasonAssigned}, true
	}
	for _, p := range in.Hints {
		if l, ok := byPort[p]; ok {
			return Choice{l, ReasonPrinted}, true
		}
	}
	if l, ok := byPort[in.Current]; ok && in.Current > 0 {
		return Choice{l, ReasonCurrent}, true
	}
	if in.Probe == nil {
		return Choice{}, false
	}
	for _, l := range in.Listeners { // already sorted by port
		if _, skip := NonHTTPPorts[l.Port]; skip {
			continue
		}
		if in.Probe(l) {
			return Choice{l, ReasonProbed}, true
		}
	}
	return Choice{}, false
}

// ProbeHTTP reports whether the listener answers an HTTP request with any
// status code. Redirects are not followed.
func ProbeHTTP(l Listener, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	addr := net.JoinHostPort(l.DialHost(), strconv.Itoa(l.Port))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		return false
	}
	req.Host = "localhost"
	client := &http.Client{
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// ProbeCache wraps a probe so a port that failed is not re-probed until ttl
// passes; a port that succeeded is remembered until Forget is called (the
// caller forgets ports that stop listening).
type ProbeCache struct {
	Probe func(Listener) bool
	TTL   time.Duration

	mu     sync.Mutex
	ok     map[int]bool
	failAt map[int]time.Time
}

func (c *ProbeCache) Check(l Listener) bool {
	c.mu.Lock()
	if c.ok == nil {
		c.ok, c.failAt = map[int]bool{}, map[int]time.Time{}
	}
	if c.ok[l.Port] {
		c.mu.Unlock()
		return true
	}
	if t, failed := c.failAt[l.Port]; failed && time.Since(t) < c.TTL {
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()

	res := c.Probe(l)

	c.mu.Lock()
	defer c.mu.Unlock()
	if res {
		c.ok[l.Port] = true
		delete(c.failAt, l.Port)
	} else {
		c.failAt[l.Port] = time.Now()
	}
	return res
}

// Retain forgets cached results for ports not in live.
func (c *ProbeCache) Retain(live []Listener) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keep := map[int]bool{}
	for _, l := range live {
		keep[l.Port] = true
	}
	for p := range c.ok {
		if !keep[p] {
			delete(c.ok, p)
		}
	}
	for p := range c.failAt {
		if !keep[p] {
			delete(c.failAt, p)
		}
	}
}
