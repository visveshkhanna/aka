//go:build unix

package daemon

import (
	"errors"
	"syscall"
)

func isAddrNotAvail(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EAFNOSUPPORT)
}
