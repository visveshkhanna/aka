package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lock takes a LockFileEx lock on the first byte. Like flock, Windows
// releases it when the handle closes, including when the process dies.
func lock(path string, block bool) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !block {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
