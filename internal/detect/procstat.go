package detect

import (
	"strconv"
	"strings"
)

// parseProcStat parses the ppid, pgrp and starttime fields of a Linux /proc/<pid>/stat
// line. The comm field is parenthesised and may itself contain spaces and
// parentheses, so parsing starts after the last ')'.
//
//	1234 (my (weird) cmd) S 1 1234 1234 ...
//	pid  comm             st ppid pgrp
//
// It lives in a file without a build tag so it is unit-tested on every OS.
func parseProcStat(pid int, line string) (Proc, bool) {
	i := strings.LastIndexByte(line, ')')
	if i < 0 {
		return Proc{}, false
	}
	f := strings.Fields(line[i+1:])
	if len(f) < 3 {
		return Proc{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	pgid, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil {
		return Proc{}, false
	}
	p := Proc{PID: pid, PPID: ppid, PGID: pgid}
	// starttime is field 22 of stat; f[0] is field 3 (state).
	if len(f) > 19 {
		p.Start, _ = strconv.ParseUint(f[19], 10, 64)
	}
	return p, true
}
