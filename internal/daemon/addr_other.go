//go:build !unix

package daemon

import "strings"

func isAddrNotAvail(err error) bool {
	return strings.Contains(err.Error(), "address")
}
