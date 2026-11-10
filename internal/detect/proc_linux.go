package detect

// listProcs reads /proc/<pid>/stat for every process.
func listProcs() ([]Proc, error) { return listProcsFrom("/proc") }
