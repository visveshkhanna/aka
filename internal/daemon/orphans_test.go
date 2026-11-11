//go:build unix

package daemon

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/visveshkhanna/aka/internal/detect"
)

// spawnChild starts a long sleep in its own process group, as the runner
// does, and registers it as the session's child. The returned channel is
// closed when it exits; it is the only waiter.
func spawnChild(t *testing.T, c *Conn) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-done })
	st, ok := detect.StartTime(cmd.Process.Pid)
	if !ok {
		t.Fatal("no start time for child")
	}
	if r, err := c.Call(Request{Op: OpChild, ChildPID: cmd.Process.Pid, ChildStart: st, ChildPGID: cmd.Process.Pid}); err != nil || !r.OK {
		t.Fatalf("child: %+v %v", r, err)
	}
	return cmd, done
}

func TestDroppedSessionKillsOrphan(t *testing.T) {
	_, dir := startDaemon(t)
	c, _ := register(t, dir, "orphan.localhost", false)
	_, done := spawnChild(t, c)
	c.Close() // as if aka were SIGKILLed
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("orphan still running after its session dropped")
	}
}

func TestDaemonShutdownLeavesAppsRunning(t *testing.T) {
	s, dir := startDaemon(t)
	c, _ := register(t, dir, "keep.localhost", false)
	cmd, done := spawnChild(t, c)
	s.Close()
	// The session is told the stop was deliberate.
	c.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if r, err := c.Recv(); err != nil || r.Event != EventStopped {
		t.Fatalf("got %+v %v, want the stopped event", r, err)
	}
	select {
	case <-done:
		t.Fatal("daemon shutdown killed a running app")
	case <-time.After(orphanGrace + time.Second):
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("app gone: %v", err)
	}
}
