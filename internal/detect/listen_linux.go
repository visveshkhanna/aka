package detect

// listeners matches LISTEN socket inodes from /proc/net/tcp{,6} against the
// socket fds of each PID. Reading another user's /proc/<pid>/fd needs
// privileges, but our own child tree is always readable.
func listeners(pids []int) ([]Listener, error) { return listenersFrom("/proc", pids) }
