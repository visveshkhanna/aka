package daemon

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/visveshkhanna/aka/internal/certs"
	"github.com/visveshkhanna/aka/internal/lockfile"
	"github.com/visveshkhanna/aka/internal/netx"
	"github.com/visveshkhanna/aka/internal/ports"
	"github.com/visveshkhanna/aka/internal/proxy"
)

// Config configures a daemon.
type Config struct {
	StateDir  string
	ProxyPort int // 0 picks a random port (tests)
	// ProxyHosts are the addresses the proxy binds. Default: 127.0.0.1 and
	// ::1, because *.localhost resolves to both and browsers try either.
	ProxyHosts []string
	MinPort    int
	MaxPort    int
	Log        *log.Logger

	// TLS enables HTTPS: the proxy port then accepts both HTTP and HTTPS,
	// with certificates from the local CA in StateDir.
	TLS bool
	// HTTPSPort is a dedicated HTTPS port to try as well (443), so URLs need
	// no port. If it cannot be bound, the daemon runs without it and reports
	// why. 0 means none.
	HTTPSPort int
	// HTTPSListeners are already-bound HTTPS listeners, used instead of
	// binding HTTPSPort. A launcher running as root binds 443 and passes
	// them in after dropping privileges.
	HTTPSListeners []net.Listener
}

// Server is a running daemon.
type Server struct {
	cfg Config
	log *log.Logger

	mu      sync.Mutex
	routes  map[string]*entry
	closing bool // Close has begun: dropped sessions are not orphans

	proxyLns  []net.Listener
	ctlLn     net.Listener
	proxyPort int

	tlsConfig  *tls.Config // nil when TLS is off
	httpsLns   []net.Listener
	httpsPort  int    // dedicated HTTPS port in use, 0 if none
	httpsErr   string // why HTTPSPort could not be used
	privileged bool   // httpsLns were handed in and cannot be rebound
	httpSrv    *http.Server
	unlock     func()
	done       chan struct{}
	closeOnce  sync.Once
}

type entry struct {
	route proxy.Route
	// assigned is the PORT handed to the child; reserved until the session
	// ends so concurrent runs never get the same port.
	assigned int
	sess     *session
	child    childInfo
}

type childInfo struct {
	pid   int
	start uint64
	pgid  int
}

type session struct {
	conn net.Conn
	wmu  sync.Mutex
	enc  *json.Encoder
	host string // hostname owned by this session ("" until registered)
}

func (s *session) send(r Response) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return s.enc.Encode(r)
}

// ErrAlreadyRunning is returned by Start when another daemon holds the lock.
var ErrAlreadyRunning = errors.New("daemon already running")

// Start acquires the daemon lock, binds the control socket and proxy ports,
// and starts serving. It returns once everything is listening.
func Start(cfg Config) (*Server, error) {
	if cfg.Log == nil {
		cfg.Log = log.New(os.Stderr, "", log.LstdFlags)
	}
	if cfg.MinPort == 0 {
		cfg.MinPort, cfg.MaxPort = ports.MinAppPort, ports.MaxAppPort
	}
	if len(cfg.ProxyHosts) == 0 {
		cfg.ProxyHosts = []string{"127.0.0.1", "::1"}
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, err
	}
	unlock, err := lockfile.TryLock(LockPath(cfg.StateDir))
	if errors.Is(err, lockfile.ErrLocked) {
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, log: cfg.Log, routes: map[string]*entry{}, unlock: unlock, done: make(chan struct{})}
	if cfg.TLS {
		ca, _, err := certs.LoadOrCreateCA(cfg.StateDir)
		if err == nil {
			var is *certs.Issuer
			if is, err = certs.NewIssuer(ca); err == nil {
				s.tlsConfig = is.TLSConfig()
			}
		}
		if err != nil {
			unlock()
			return nil, err
		}
	}
	s.loadAliases()
	if err := s.listen(); err != nil {
		s.closeListeners()
		unlock()
		return nil, err
	}
	p := proxy.New(s)
	p.Log = s.log
	p.OnHint = s.sendHint
	s.httpSrv = &http.Server{Handler: p, TLSConfig: s.tlsConfig, ReadHeaderTimeout: 30 * time.Second, ErrorLog: log.New(discardWriter{}, "", 0)}
	for _, ln := range s.proxyLns {
		if s.tlsConfig == nil {
			go s.httpSrv.Serve(ln)
			continue
		}
		plain, tlsRaw := proxy.SplitTLS(ln)
		go s.httpSrv.Serve(plain)
		go s.httpSrv.ServeTLS(tlsRaw, "", "")
	}
	for _, ln := range s.httpsLns {
		go s.httpSrv.ServeTLS(ln, "", "")
	}
	go s.acceptControl()
	mode := "HTTP"
	if s.tlsConfig != nil {
		mode = "HTTP and HTTPS"
	}
	s.log.Printf("daemon pid %d: proxy on port %d (%s), control socket %s", os.Getpid(), s.proxyPort, mode, SocketPath(cfg.StateDir))
	switch {
	case s.httpsPort > 0:
		s.log.Printf("HTTPS also on port %d", s.httpsPort)
	case s.httpsErr != "":
		s.log.Printf("HTTPS port %d not used: %s", cfg.HTTPSPort, s.httpsErr)
	}
	return s, nil
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func (s *Server) listen() error {
	port := s.cfg.ProxyPort
	for i, h := range s.cfg.ProxyHosts {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, strconv.Itoa(port)))
		if err != nil {
			if i > 0 && isAddrNotAvail(err) {
				continue // no IPv6 loopback on this host
			}
			return fmt.Errorf("proxy cannot listen on %s port %d: %w", h, port, err)
		}
		if port == 0 {
			port = ln.Addr().(*net.TCPAddr).Port
		}
		s.proxyLns = append(s.proxyLns, ln)
	}
	s.proxyPort = port

	if s.cfg.TLS {
		if len(s.cfg.HTTPSListeners) > 0 {
			s.httpsLns = s.cfg.HTTPSListeners
			s.httpsPort = s.httpsLns[0].Addr().(*net.TCPAddr).Port
			s.privileged = true
		} else if s.cfg.HTTPSPort > 0 {
			s.listenHTTPS(s.cfg.HTTPSPort)
		}
	}

	sock := SocketPath(s.cfg.StateDir)
	// We hold the lock, so any socket file left here is from a dead daemon.
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	os.Chmod(sock, 0o600)
	s.ctlLn = ln
	return nil
}

// listenHTTPS binds the dedicated HTTPS port on every proxy host. Failure
// is not fatal: the proxy port still serves HTTPS, so the daemon records why
// and carries on.
func (s *Server) listenHTTPS(port int) {
	var lns []net.Listener
	for i, h := range s.cfg.ProxyHosts {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, strconv.Itoa(port)))
		if err != nil {
			if i > 0 && isAddrNotAvail(err) {
				continue
			}
			for _, l := range lns {
				l.Close()
			}
			s.httpsErr = describeListenError(err, port)
			return
		}
		lns = append(lns, ln)
	}
	s.httpsLns, s.httpsPort = lns, port
}

func describeListenError(err error, port int) string {
	switch {
	case netx.IsPermission(err):
		return "binding port " + strconv.Itoa(port) + " needs privileges"
	case netx.IsAddrInUse(err):
		return "port " + strconv.Itoa(port) + " is in use by another program"
	}
	return err.Error()
}

// ProxyPort returns the port the proxy is listening on.
func (s *Server) ProxyPort() int { return s.proxyPort }

// Done is closed when the daemon has shut down.
func (s *Server) Done() <-chan struct{} { return s.done }

// Close stops the daemon: listeners, sessions and the lock.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.closeListeners()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		s.httpSrv.Shutdown(ctx)
		cancel()
		s.mu.Lock()
		s.closing = true
		for _, e := range s.routes {
			if e.sess != nil {
				e.sess.send(Response{OK: true, Event: EventStopped})
				e.sess.conn.Close()
			}
		}
		s.mu.Unlock()
		os.Remove(SocketPath(s.cfg.StateDir))
		s.unlock()
		close(s.done)
	})
}

func (s *Server) closeListeners() {
	for _, ln := range s.proxyLns {
		ln.Close()
	}
	for _, ln := range s.httpsLns {
		ln.Close()
	}
	if s.ctlLn != nil {
		s.ctlLn.Close()
	}
}

func (s *Server) pingResponse() Response {
	return Response{
		OK: true, PID: os.Getpid(), ProxyPort: s.proxyPort, BuildID: BuildID(),
		TLS: s.tlsConfig != nil, HTTPSPort: s.httpsPort, HTTPSError: s.httpsErr, Privileged: s.privileged,
	}
}

// sendHint passes advice about a route to the aka running that app.
func (s *Server) sendHint(rt proxy.Route, hint string) {
	s.mu.Lock()
	e, ok := s.routes[rt.Hostname]
	var sess *session
	if ok {
		sess = e.sess
	}
	s.mu.Unlock()
	if sess != nil {
		go sess.send(Response{OK: true, Event: EventHint, Message: hint})
	}
}

// Lookup implements proxy.Table.
func (s *Server) Lookup(host string) (proxy.Route, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.routes[host]
	if !ok {
		return proxy.Route{}, false
	}
	return e.route, true
}

// Routes implements proxy.Table.
func (s *Server) Routes() []proxy.Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]proxy.Route, 0, len(s.routes))
	for _, e := range s.routes {
		out = append(out, e.route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

func (s *Server) acceptControl() {
	for {
		c, err := s.ctlLn.Accept()
		if err != nil {
			return
		}
		go s.serveConn(c)
	}
}

func (s *Server) serveConn(c net.Conn) {
	sess := &session{conn: c, enc: json.NewEncoder(c)}
	defer func() {
		c.Close()
		s.endSession(sess)
	}()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			sess.send(Response{Error: "bad request: " + err.Error(), Code: CodeInvalid})
			return
		}
		resp, keep := s.handle(sess, req)
		if err := sess.send(resp); err != nil || !keep {
			return
		}
	}
}

// handle processes one request. keep=false closes the connection afterwards.
func (s *Server) handle(sess *session, req Request) (Response, bool) {
	s.mu.Lock()
	inSession := sess.host != ""
	s.mu.Unlock()
	switch req.Op {
	case OpPing:
		return s.pingResponse(), inSession
	case OpList:
		return Response{OK: true, Routes: s.Routes()}, inSession
	case OpRegister:
		return s.register(sess, req)
	case OpUpdate:
		return s.update(sess, req), true
	case OpChild:
		return s.setChild(sess, req), true
	case OpAlias:
		return s.alias(req), inSession
	case OpShutdown:
		go func() {
			time.Sleep(50 * time.Millisecond) // let the response flush
			s.log.Printf("shutdown requested")
			s.Close()
		}()
		return Response{OK: true}, false
	}
	return Response{Error: "unknown op " + strconv.Quote(req.Op), Code: CodeInvalid}, inSession
}

func (s *Server) register(sess *session, req Request) (Response, bool) {
	if req.Hostname == "" {
		return Response{Error: "hostname required", Code: CodeInvalid}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.host != "" {
		return Response{Error: "session already registered " + sess.host, Code: CodeInvalid}, true
	}
	if old, ok := s.routes[req.Hostname]; ok {
		if old.isAlias() {
			return aliasConflict(old), false // even with --force: aliases are deliberate
		}
		if !req.Force {
			return Response{
				Error:    fmt.Sprintf("%s is already in use by %s (pid %d)", req.Hostname, old.route.Command, old.route.PID),
				Code:     CodeConflict,
				OwnerPID: old.route.PID,
			}, false
		}
		if old.sess != nil {
			old.sess.host = "" // its disconnect must not remove our route
			go func(o *session) {
				o.send(Response{Event: EventEvicted, OwnerPID: req.PID})
				o.conn.Close()
			}(old.sess)
		}
		delete(s.routes, req.Hostname)
		s.log.Printf("%s taken over by pid %d (--force)", req.Hostname, req.PID)
	}
	assigned, err := s.pickPortLocked(req.PreferPort)
	if err != nil {
		return Response{Error: err.Error()}, false
	}
	s.routes[req.Hostname] = &entry{
		route:    proxy.Route{Hostname: req.Hostname, PID: req.PID, Command: req.Command, Since: time.Now()},
		assigned: assigned,
		sess:     sess,
	}
	sess.host = req.Hostname
	s.log.Printf("registered %s (pid %d, PORT=%d): %s", req.Hostname, req.PID, assigned, req.Command)
	return Response{OK: true, Assigned: assigned}, true
}

func (s *Server) pickPortLocked(prefer int) (int, error) {
	reserved := map[int]bool{}
	for _, e := range s.routes {
		reserved[e.assigned] = true
		reserved[e.route.Port] = true
	}
	reserved[s.proxyPort] = true
	if prefer > 0 && !reserved[prefer] && !ports.Blocked[prefer] {
		// The previous child may still be bound to it (daemon restart while
		// the app runs), so do not require it to be free.
		return prefer, nil
	}
	return ports.Pick(s.cfg.MinPort, s.cfg.MaxPort, func(p int) bool { return reserved[p] })
}

func (s *Server) update(sess *session, req Request) Response {
	if req.Port < 0 || req.Port > 65535 {
		return Response{Error: "invalid port", Code: CodeInvalid}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.host == "" {
		return Response{Error: "not registered (route was taken over or removed)", Code: CodeConflict}
	}
	e, ok := s.routes[sess.host]
	if !ok || e.sess != sess {
		return Response{Error: "route no longer owned by this session", Code: CodeConflict}
	}
	if e.route.Port != req.Port || e.route.DialHost != req.DialHost {
		s.log.Printf("%s -> %s", sess.host, net.JoinHostPort(orStar(req.DialHost), strconv.Itoa(req.Port)))
	}
	e.route.Port, e.route.DialHost = req.Port, req.DialHost
	return Response{OK: true}
}

func orStar(h string) string {
	if h == "" {
		return "*"
	}
	return h
}

func (s *Server) setChild(sess *session, req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.routes[sess.host]
	if sess.host == "" || !ok || e.sess != sess {
		return Response{Error: "not registered", Code: CodeInvalid}
	}
	e.child = childInfo{pid: req.ChildPID, start: req.ChildStart, pgid: req.ChildPGID}
	return Response{OK: true}
}

func (s *Server) endSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.host == "" {
		return
	}
	if e, ok := s.routes[sess.host]; ok && e.sess == sess {
		delete(s.routes, sess.host)
		s.log.Printf("removed %s", sess.host)
		if e.child.pid > 0 && !s.closing {
			// After a clean exit the child is already gone and this is a
			// no-op; the start time guards against PID reuse. When the
			// daemon itself is shutting down, the connection dropped on our
			// side and its aka is alive: leave the app alone.
			go killOrphans(s.log, sess.host, e.child)
		}
	}
	sess.host = ""
}
