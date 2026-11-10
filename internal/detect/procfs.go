package detect

// Linux procfs readers. They take the procfs root as a parameter and carry no
// build tag, so the tests exercise them against a fake /proc on every OS.

import (
	"os"
	"path/filepath"
	"strconv"
)

func listProcsFrom(root string) ([]Proc, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := make([]Proc, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue // process exited mid-scan
		}
		if p, ok := parseProcStat(pid, string(data)); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func listenersFrom(root string, pids []int) ([]Listener, error) {
	byInode := map[uint64]procNetEntry{}
	var firstErr error
	for _, f := range []struct {
		name string
		v6   bool
	}{{"tcp", false}, {"tcp6", true}} {
		data, err := os.ReadFile(filepath.Join(root, "net", f.name))
		if err != nil {
			if firstErr == nil && !os.IsNotExist(err) {
				firstErr = err
			}
			continue // tcp6 is absent when IPv6 is disabled
		}
		for _, e := range parseProcNetTCP(string(data), f.v6) {
			byInode[e.Inode] = e
		}
	}
	if len(byInode) == 0 {
		return nil, firstErr
	}
	var out []Listener
	for _, pid := range pids {
		fdDir := filepath.Join(root, strconv.Itoa(pid), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := parseSocketInode(link)
			if !ok {
				continue
			}
			if e, ok := byInode[inode]; ok {
				out = append(out, Listener{PID: pid, IP: e.IP, IPv6: e.IPv6, Port: e.Port})
			}
		}
	}
	return out, nil
}
