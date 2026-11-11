package runner

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/daemon"
)

// ConflictError means the hostname is owned by another running aka.
type ConflictError struct {
	Hostname string
	Message  string
	OwnerPID int
}

func (e *ConflictError) Error() string { return e.Message }

// session holds this run's registration with the daemon. If the daemon
// goes away (crash, upgrade) the session reconnects, re-registers with the
// same assigned port and replays the current upstream, so the child never
// notices.
type session struct {
	start    daemon.StartOptions
	hostname string
	pid      int
	command  string
	force    bool
	logf     func(string, ...any)

	mu       sync.Mutex
	conn     *daemon.Conn
	assigned int
	port     int
	dialHost string
	closed   bool
	waiting  bool            // the daemon was stopped on purpose; wait, do not start one
	child    daemon.Request  // last OpChild, replayed on reconnect
	evicted  chan int        // receives the evicting PID
	ping     daemon.Response // from the daemon the session registered with
}

func newSession(start daemon.StartOptions, hostname string, pid int, command string, force bool, logf func(string, ...any)) *session {
	return &session{start: start, hostname: hostname, pid: pid, command: command, force: force, logf: logf, evicted: make(chan int, 1)}
}

// open ensures the daemon is running and registers. prefer is a port to
// reuse (0 for a fresh one).
func (s *session) open(prefer int, force bool) error {
	ping, err := daemon.Ensure(s.start)
	if err != nil {
		return err
	}
	c, err := daemon.Dial(s.start.StateDir)
	if err != nil {
		return err
	}
	r, err := c.Call(daemon.Request{
		Op: daemon.OpRegister, Hostname: s.hostname, PID: s.pid, Command: s.command,
		Force: force, PreferPort: prefer,
	})
	if err != nil {
		c.Close()
		return err
	}
	if !r.OK {
		c.Close()
		if r.Code == daemon.CodeConflict {
			return &ConflictError{Hostname: s.hostname, Message: r.Error, OwnerPID: r.OwnerPID}
		}
		return errors.New(r.Error)
	}
	s.mu.Lock()
	s.conn, s.assigned, s.ping = c, r.Assigned, ping
	s.mu.Unlock()
	go s.read(c)
	return nil
}

// read consumes replies and events. Update replies need no handling: a
// failed update means the session is gone, which the read error reports.
func (s *session) read(c *daemon.Conn) {
	for {
		r, err := c.Recv()
		if err != nil {
			break
		}
		if r.Event == daemon.EventHint {
			s.logf("%s", r.Message)
			continue
		}
		if r.Event == daemon.EventStopped {
			s.mu.Lock()
			s.waiting = true
			s.mu.Unlock()
			break
		}
		if r.Event == daemon.EventEvicted {
			s.mu.Lock()
			s.closed = true
			s.mu.Unlock()
			select {
			case s.evicted <- r.OwnerPID:
			default:
			}
			c.Close()
			return
		}
	}
	c.Close()
	s.mu.Lock()
	stale := s.conn != c || s.closed
	if !stale {
		s.conn = nil
	}
	s.mu.Unlock()
	if !stale {
		go s.reconnect()
	}
}

// reconnect re-registers after the connection drops. After a crash it
// starts a new daemon at once. After a deliberate stop (`aka proxy stop`)
// it only waits for one to appear, so stopping the proxy with apps running
// is not undone behind the user's back; the app keeps running unrouted.
func (s *session) reconnect() {
	s.mu.Lock()
	waiting := s.waiting
	s.mu.Unlock()
	if waiting {
		s.logf("the proxy was stopped; this app keeps running and is routed again when the proxy starts (`%s proxy start`)", brand.Name)
	} else {
		s.logf("lost connection to the proxy daemon; reconnecting")
	}
	delay := 200 * time.Millisecond
	for {
		s.mu.Lock()
		closed, prefer := s.closed, s.assigned
		s.mu.Unlock()
		if closed {
			return
		}
		if waiting {
			if _, err := daemon.Ping(s.start.StateDir); err != nil {
				time.Sleep(time.Second)
				continue
			}
		}
		err := s.open(prefer, false)
		if err == nil {
			s.mu.Lock()
			port, host, child := s.port, s.dialHost, s.child
			s.mu.Unlock()
			if child.Op != "" {
				s.sendReq(child)
			}
			if port > 0 {
				s.send(port, host)
			}
			s.mu.Lock()
			s.waiting = false
			s.mu.Unlock()
			s.logf("reconnected to the proxy daemon")
			return
		}
		var ce *ConflictError
		if errors.As(err, &ce) {
			s.logf("cannot re-register: %v", err)
			return
		}
		time.Sleep(delay)
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

// setUpstream records and publishes the detected upstream.
func (s *session) setUpstream(port int, dialHost string) {
	s.mu.Lock()
	s.port, s.dialHost = port, dialHost
	s.mu.Unlock()
	s.send(port, dialHost)
}

// setChild reports the spawned child so the daemon can clean it up if this
// aka process is killed without a chance to do so itself.
func (s *session) setChild(pid int, start uint64, pgid int) {
	req := daemon.Request{Op: daemon.OpChild, ChildPID: pid, ChildStart: start, ChildPGID: pgid}
	s.mu.Lock()
	s.child = req
	s.mu.Unlock()
	s.sendReq(req)
}

func (s *session) send(port int, dialHost string) {
	s.sendReq(daemon.Request{Op: daemon.OpUpdate, Port: port, DialHost: dialHost})
}

func (s *session) sendReq(req daemon.Request) {
	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	if c == nil {
		return // reconnect will replay it
	}
	if err := c.Send(req); err != nil {
		c.Close() // read() notices and reconnects
	}
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

// info returns the assigned port and the daemon's ping from the first open.
func (s *session) info() (assigned int, ping daemon.Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.assigned, s.ping
}

func conflictHelp(e *ConflictError, tool string) string {
	return fmt.Sprintf("%s\nUse a different name, or `%s --force ...` to take it over.", e.Message, tool)
}
