package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"aka/internal/brand"
)

// Conn is a client connection to the daemon.
type Conn struct {
	c   net.Conn
	sc  *bufio.Scanner
	enc *json.Encoder
}

// Dial connects to the daemon in stateDir.
func Dial(stateDir string) (*Conn, error) {
	c, err := net.DialTimeout("unix", SocketPath(stateDir), 2*time.Second)
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	return &Conn{c: c, sc: sc, enc: json.NewEncoder(c)}, nil
}

func (c *Conn) Close() error { return c.c.Close() }

// Send writes a request without waiting for a reply.
func (c *Conn) Send(req Request) error {
	c.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.enc.Encode(req)
}

// Recv reads the next message from the daemon.
func (c *Conn) Recv() (Response, error) {
	if !c.sc.Scan() {
		if err := c.sc.Err(); err != nil {
			return Response{}, err
		}
		return Response{}, errors.New("daemon closed the connection")
	}
	var r Response
	err := json.Unmarshal(c.sc.Bytes(), &r)
	return r, err
}

// Call sends a request and waits for the reply, with a timeout.
func (c *Conn) Call(req Request) (Response, error) {
	if err := c.Send(req); err != nil {
		return Response{}, err
	}
	c.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer c.c.SetReadDeadline(time.Time{})
	return c.Recv()
}

// Do opens a connection, makes one call and closes it.
func Do(stateDir string, req Request) (Response, error) {
	c, err := Dial(stateDir)
	if err != nil {
		return Response{}, err
	}
	defer c.Close()
	return c.Call(req)
}

// Ping returns the daemon's ping response, or an error if none is running.
func Ping(stateDir string) (Response, error) { return Do(stateDir, Request{Op: OpPing}) }

// StartOptions controls how Ensure launches a daemon.
type StartOptions struct {
	StateDir  string
	ProxyPort int
	// Args are passed to the executable to run the daemon in the foreground
	// (e.g. []string{"__daemon"}).
	Args []string
	// Env is appended to the daemon's environment.
	Env []string
	// Warn receives non-fatal notices (e.g. a stale daemon build).
	Warn func(string)
}

// Ensure returns a ping from a running daemon, starting one if needed. When
// the running daemon is a different build and has no routes, it is restarted
// so the new code takes effect.
func Ensure(o StartOptions) (Response, error) {
	if r, err := Ping(o.StateDir); err == nil {
		if r.BuildID == BuildID() {
			return r, nil
		}
		list, lerr := Do(o.StateDir, Request{Op: OpList})
		if lerr == nil && len(list.Routes) == 0 {
			if err := Stop(o.StateDir, 3*time.Second); err == nil {
				return start(o)
			}
		}
		if o.Warn != nil {
			o.Warn("the running proxy daemon is an older build; run `" + brand.Name + " proxy stop` when its apps are done to upgrade it")
		}
		return r, nil
	}
	return start(o)
}

func start(o StartOptions) (Response, error) {
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return Response{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Response{}, err
	}
	logPath := LogPath(o.StateDir)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Response{}, err
	}
	defer logf.Close()
	startOffset, _ := logf.Seek(0, 2)

	cmd := exec.Command(exe, o.Args...)
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Dir = "/"
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return Response{}, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := Ping(o.StateDir); err == nil {
			return r, nil
		}
		select {
		case <-exited:
			// Lost a race with another starter? Its daemon will answer.
			if r, err := Ping(o.StateDir); err == nil {
				return r, nil
			}
			return Response{}, fmt.Errorf("proxy daemon failed to start:\n%s", logTail(logPath, startOffset))
		case <-time.After(50 * time.Millisecond):
		}
	}
	return Response{}, fmt.Errorf("proxy daemon did not answer within 5s; see %s", logPath)
}

func logTail(path string, from int64) string {
	b, err := os.ReadFile(path)
	if err != nil || int64(len(b)) < from {
		return "(no log)"
	}
	s := strings.TrimSpace(string(b[from:]))
	if s == "" {
		return "(no output; see " + path + ")"
	}
	return s
}

// Stop asks the daemon to shut down and waits for its socket to go away.
func Stop(stateDir string, wait time.Duration) error {
	if _, err := Do(stateDir, Request{Op: OpShutdown}); err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if _, err := Ping(stateDir); err != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("daemon did not stop in time")
}
