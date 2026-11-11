package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/compose"
	"github.com/visveshkhanna/aka/internal/daemon"
	"github.com/visveshkhanna/aka/internal/detect"
)

// composePoll is how often `docker compose ps` is read.
var composePoll = 2 * time.Second

// cmdCompose runs `docker compose <args>` (default: up) and, while it runs,
// routes <service>.<project>.localhost to each service's published port.
// With `up -d` compose returns at once; aka then keeps routing until
// Ctrl+C.
func cmdCompose(args []string) int {
	global, rest := compose.SplitArgs(args)
	docker, err := exec.LookPath("docker")
	if err != nil {
		return fail(errors.New("docker is not installed or not on PATH"))
	}
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	if so.TLS {
		ensureTrust(so.StateDir)
	}
	ping, err := daemon.Ensure(so)
	if err != nil {
		return fail(err)
	}

	cmd := exec.Command(docker, append(append([]string{"compose"}, global...), rest...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// compose stays in our process group: Ctrl+C reaches it from the
	// terminal, and it stops its containers as it would without aka.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	r := &composeRouter{dir: so.StateDir, ping: ping, routes: map[string]*composeRoute{}}
	defer r.closeAll()
	ps := func() {
		out, err := exec.Command(docker, append(append([]string{"compose"}, global...), "ps", "--format", "json")...).Output()
		if err != nil {
			return // the project may not exist yet
		}
		svcs, err := compose.ParsePS(out)
		if err != nil {
			notice("cannot read docker compose ps: %v", err)
			return
		}
		r.sync(svcs)
	}
	tick := time.NewTicker(composePoll)
	defer tick.Stop()
	running, code := true, 0
	for {
		select {
		case err := <-exited:
			running = false
			code = exitStatus(err)
			if code != 0 || !compose.Detached(rest) {
				return code
			}
			ps()
			notice("containers run in the background; routing them until Ctrl+C")
		case sig := <-sigs:
			if !running {
				return code // detached: Ctrl+C just stops routing
			}
			if sig != os.Interrupt {
				cmd.Process.Signal(sig) // the terminal delivered Ctrl+C itself
			}
		case <-tick.C:
			ps()
		}
	}
}

// composeRouter keeps one daemon session per published service, so each
// route goes away when its service stops or aka exits.
type composeRouter struct {
	dir    string
	ping   daemon.Response
	mu     sync.Mutex
	routes map[string]*composeRoute
	warned map[string]bool
}

type composeRoute struct {
	conn *daemon.Conn
	port int
	dead chan struct{}
}

func (r *composeRouter) sync(svcs []compose.Service) {
	seen := map[string]bool{}
	for _, s := range svcs {
		host := s.Hostname(brand.DefaultTLD)
		seen[host] = true
		port := choosePort(s.Ports)
		r.mu.Lock()
		cur := r.routes[host]
		r.mu.Unlock()
		if cur != nil {
			select {
			case <-cur.dead: // daemon restarted: register again below
				cur = nil
			default:
			}
		}
		switch {
		case cur == nil:
			r.add(host, s, port)
		case cur.port != port:
			cur.conn.Send(daemon.Request{Op: daemon.OpUpdate, Port: port})
			cur.port = port
			notice("%s -> port %d (compose service %s)", r.ping.URL(host), port, s.Service)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for host, rt := range r.routes {
		if !seen[host] {
			rt.conn.Close()
			delete(r.routes, host)
			notice("%s removed (service stopped)", r.ping.URL(host))
		}
	}
}

func (r *composeRouter) add(host string, s compose.Service, port int) {
	c, err := daemon.Dial(r.dir)
	if err != nil {
		return
	}
	resp, err := c.Call(daemon.Request{Op: daemon.OpRegister, Hostname: host, PID: os.Getpid(), Command: "docker compose: " + s.Service})
	if err != nil || !resp.OK {
		c.Close()
		r.mu.Lock()
		if r.warned == nil {
			r.warned = map[string]bool{}
		}
		first := !r.warned[host]
		r.warned[host] = true
		r.mu.Unlock()
		if first && err == nil {
			notice("cannot route %s: %s", host, resp.Error)
		}
		return
	}
	c.Send(daemon.Request{Op: daemon.OpUpdate, Port: port})
	rt := &composeRoute{conn: c, port: port, dead: make(chan struct{})}
	go func() { // notice the daemon going away
		for {
			if _, err := c.Recv(); err != nil {
				close(rt.dead)
				return
			}
		}
	}()
	r.mu.Lock()
	r.routes[host] = rt
	r.mu.Unlock()
	notice("%s -> port %d (compose service %s)", r.ping.URL(host), port, s.Service)
}

func (r *composeRouter) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for host, rt := range r.routes {
		rt.conn.Close()
		delete(r.routes, host)
	}
}

// choosePort picks the first published port that answers HTTP, else the
// first one (a database is still worth a name, e.g. for tools).
func choosePort(ports []int) int {
	if len(ports) > 1 {
		for _, p := range ports {
			if detect.ProbeHTTP(detect.Listener{Port: p}, 300*time.Millisecond) {
				return p
			}
		}
	}
	return ports[0]
}
