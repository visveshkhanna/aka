//go:build !unix && !windows

package lockfile

import (
	"errors"
	"os"
	"time"
)

// lock on Windows relies on exclusive file creation semantics. Proper
// LockFileEx support is part of the Windows build step.
func lock(path string, block bool) (func(), error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		if !block {
			return nil, ErrLocked
		}
		time.Sleep(50 * time.Millisecond)
	}
}
