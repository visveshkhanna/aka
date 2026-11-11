// Package netx classifies network errors the same way on every platform.
// On Windows, sockets fail with WSA error codes (WSAECONNREFUSED, ...)
// that do not match syscall.ECONNREFUSED and friends.
package netx

import (
	"errors"
	"syscall"
)

// IsRefused reports a refused connection: nothing listening on the port.
func IsRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || isWSA(err, wsaConnRefused)
}

// IsAddrInUse reports a bind to a port another socket holds.
func IsAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || isWSA(err, wsaAddrInUse)
}

// IsPermission reports a bind refused for lack of privileges.
func IsPermission(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || isWSA(err, wsaAccess)
}

// WSA codes (winerror.h). Compared only on Windows; see isWSA.
const (
	wsaAccess      = 10013
	wsaAddrInUse   = 10048
	wsaConnRefused = 10061
)
