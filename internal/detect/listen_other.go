//go:build !darwin && !linux && !windows

package detect

import (
	"errors"
	"runtime"
)

// Detection is implemented for macOS, Linux and Windows. Elsewhere it is
// unavailable and routing relies on the assigned PORT and stdout hints.
var errUnsupported = errors.New("listener detection is not supported on " + runtime.GOOS + " yet")

func listeners([]int) ([]Listener, error) { return nil, errUnsupported }

func listProcs() ([]Proc, error) { return nil, errUnsupported }
