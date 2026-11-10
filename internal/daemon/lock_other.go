//go:build !unix

package daemon

import (
	"errors"
	"os"
	"strings"
)

// lockFile on Windows relies on exclusive file creation semantics. Proper
// LockFileEx support is part of the Windows build step.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}

func isAddrNotAvail(err error) bool {
	return strings.Contains(err.Error(), "address")
}
