package netx

import (
	"errors"
	"syscall"
)

func isWSA(err error, code syscall.Errno) bool { return errors.Is(err, code) }
