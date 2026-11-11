//go:build !windows

package netx

import "syscall"

// isWSA is false off Windows, where the same numbers could mean something
// else.
func isWSA(error, syscall.Errno) bool { return false }
