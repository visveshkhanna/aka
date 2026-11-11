//go:build !unix && !windows

package lockfile

import (
	"errors"
	"os"
	"time"
)

// lock is the fallback for platforms with neither flock nor LockFileEx: it
// retries opening the file until it succeeds.
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
