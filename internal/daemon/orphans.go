package daemon

import (
	"log"
	"os"
	"time"

	"github.com/visveshkhanna/aka/internal/detect"
)

// orphanGrace is how long orphans get to exit after SIGTERM before SIGKILL.
var orphanGrace = 3 * time.Second

// killOrphans terminates a session's child tree if it outlived its aka
// process. The child is matched by PID and start time, never by PID alone.
func killOrphans(lg *log.Logger, host string, c childInfo) {
	pids := orphanPIDs(c)
	if len(pids) == 0 {
		return
	}
	lg.Printf("%s: aka exited without cleaning up; stopping orphaned pids %v", host, pids)
	signalPIDs(pids, false)
	deadline := time.Now().Add(orphanGrace)
	for time.Now().Before(deadline) {
		if len(orphanPIDs(c)) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	signalPIDs(orphanPIDs(c), true)
}

func orphanPIDs(c childInfo) []int {
	procs, err := detect.ListProcs()
	if err != nil {
		return nil
	}
	alive := false
	for _, p := range procs {
		if p.PID == c.pid && p.Start == c.start {
			alive = true
			break
		}
	}
	if !alive {
		return nil
	}
	return detect.Tree(procs, c.pid, c.pgid, os.Getpid())
}
