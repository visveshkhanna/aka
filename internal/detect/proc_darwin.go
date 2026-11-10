package detect

import "golang.org/x/sys/unix"

// listProcs reads the process table with sysctl(kern.proc.all), which needs no
// privileges and no subprocess.
func listProcs() ([]Proc, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	out := make([]Proc, 0, len(kps))
	for _, kp := range kps {
		out = append(out, Proc{
			PID:   int(kp.Proc.P_pid),
			PPID:  int(kp.Eproc.Ppid),
			PGID:  int(kp.Eproc.Pgid),
			Start: uint64(kp.Proc.P_starttime.Sec)*1e6 + uint64(kp.Proc.P_starttime.Usec),
		})
	}
	return out, nil
}
