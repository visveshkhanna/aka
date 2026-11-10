//go:build darwin || freebsd || netbsd || openbsd || dragonfly || linux

package runner

import (
	"os"

	"golang.org/x/sys/unix"
)

// rawOutput turns off output post-processing (ONLCR) on the child's pty, so
// bytes reach our terminal unchanged and are translated once, by it, rather
// than twice ("\r\r\n").
func rawOutput(tty *os.File) {
	fd := int(tty.Fd())
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return
	}
	t.Oflag &^= unix.OPOST
	unix.IoctlSetTermios(fd, ioctlSetTermios, t)
}
