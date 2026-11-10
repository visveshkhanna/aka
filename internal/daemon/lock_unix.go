//go:build unix

package daemon

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive, non-blocking flock. The kernel releases it if
// the daemon dies, so there is never a stale lock to clean up.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}

func isAddrNotAvail(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EAFNOSUPPORT)
}
