// Package ports picks free TCP ports for child apps.
package ports

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"syscall"
)

// Range is the default port range handed to apps (same as portless).
const (
	MinAppPort = 4000
	MaxAppPort = 4999
)

// Blocked are ports browsers refuse to connect to (WHATWG fetch "bad ports").
// An app handed one of these would be unreachable from the browser.
var Blocked = map[int]bool{}

func init() {
	for _, p := range []int{
		0, 1, 7, 9, 11, 13, 15, 17, 19, 20, 21, 22, 23, 25, 37, 42, 43, 53, 69, 77, 79, 87, 95, 101, 102,
		103, 104, 109, 110, 111, 113, 115, 117, 119, 123, 135, 137, 139, 143, 161, 179, 389, 427, 465,
		512, 513, 514, 515, 526, 530, 531, 532, 540, 548, 554, 556, 563, 587, 601, 636, 989, 990, 993,
		995, 1719, 1720, 1723, 2049, 3659, 4045, 4190, 5060, 5061, 6000, 6566, 6665, 6666, 6667, 6668,
		6669, 6679, 6697, 10080,
	} {
		Blocked[p] = true
	}
}

// probeHosts are checked one at a time: an app may bind any of them, and on
// BSD-style stacks a wildcard bind can succeed while a specific address on
// the same port is taken, so each must be free individually.
var probeHosts = []string{"127.0.0.1", "::1", "0.0.0.0", "::"}

// IsFree reports whether port can be bound on every loopback and wildcard
// address. An address family missing from the host does not count against it.
func IsFree(port int) bool {
	for _, h := range probeHosts {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, strconv.Itoa(port)))
		if err != nil {
			if errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EAFNOSUPPORT) {
				continue
			}
			return false
		}
		ln.Close()
	}
	return true
}

// Pick returns a free port in [min,max] that is not blocked and for which
// skip returns false (used to avoid ports already handed to other apps that
// have not bound yet). It tries random ports first, then scans.
func Pick(min, max int, skip func(int) bool) (int, error) {
	if min > max {
		return 0, fmt.Errorf("invalid port range %d-%d", min, max)
	}
	ok := func(p int) bool {
		return !Blocked[p] && (skip == nil || !skip(p)) && IsFree(p)
	}
	for i := 0; i < 50; i++ {
		p := min + rand.IntN(max-min+1)
		if ok(p) {
			return p, nil
		}
	}
	for p := min; p <= max; p++ {
		if ok(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in range %d-%d", min, max)
}
