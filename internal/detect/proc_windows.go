package detect

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listProcs walks a Toolhelp32 snapshot of the process table. Start is the
// process creation time (100 ns ticks since 1601), read with the
// least-privileged query right; processes we may not open get 0, which
// still identifies them across a snapshot pair well enough to skip them.
// Windows has no process groups, so PGID is always 0.
func listProcs() ([]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Proc
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out = append(out, Proc{PID: int(e.ProcessID), PPID: int(e.ParentProcessID), Start: creationTime(e.ProcessID)})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return out, err
	}
	return out, nil
}

func creationTime(pid uint32) uint64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var c, x, k, u windows.Filetime
	if windows.GetProcessTimes(h, &c, &x, &k, &u) != nil {
		return 0
	}
	return uint64(c.HighDateTime)<<32 | uint64(c.LowDateTime)
}
