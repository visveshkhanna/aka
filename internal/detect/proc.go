// Package detect finds the TCP port a dev server actually bound, by looking
// at LISTEN sockets owned by the process tree we spawned and at the URLs the
// server prints.
package detect

import "sort"

// Proc is the minimal process-table entry needed to build a process tree.
type Proc struct {
	PID  int
	PPID int
	PGID int
	// Start is an OS-specific start-time stamp. Together with PID it
	// identifies a process across PID reuse; only equality is meaningful.
	Start uint64
}

// ListProcs returns a snapshot of the process table. Implemented per OS.
func ListProcs() ([]Proc, error) { return listProcs() }

// StartTime returns the start-time stamp of pid.
func StartTime(pid int) (uint64, bool) {
	procs, err := ListProcs()
	if err != nil {
		return 0, false
	}
	for _, p := range procs {
		if p.PID == pid {
			return p.Start, true
		}
	}
	return 0, false
}

// Tree returns root and every process descended from it, plus every process
// in process group pgid (pass 0 to skip the group match). The group match
// catches servers that double-fork and get reparented to init/launchd, which
// drops them out of the parent chain but keeps them in our process group.
// exclude removes one PID from the result (typically our own).
//
// The result is sorted and contains root even if root is not in procs
// (it may have exited between the snapshot and the call).
func Tree(procs []Proc, root, pgid, exclude int) []int {
	children := make(map[int][]int, len(procs))
	for _, p := range procs {
		if p.PID != p.PPID { // guard against PID 0 / self-parented entries
			children[p.PPID] = append(children[p.PPID], p.PID)
		}
	}
	seen := map[int]bool{}
	var walk func(int)
	walk = func(pid int) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		for _, c := range children[pid] {
			walk(c)
		}
	}
	if root > 0 {
		walk(root)
	}
	if pgid > 0 {
		for _, p := range procs {
			if p.PGID == pgid {
				walk(p.PID)
			}
		}
	}
	delete(seen, exclude)
	delete(seen, 0)
	out := make([]int, 0, len(seen))
	for pid := range seen {
		out = append(out, pid)
	}
	sort.Ints(out)
	return out
}
