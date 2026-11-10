// Package privdrop lets a command started with sudo do one privileged thing
// (bind port 443) and then continue as the user who ran sudo, so every file
// it creates and every process it starts belongs to that user.
package privdrop

import (
	"errors"
	"os"
	"os/user"
)

// ErrUnsupported is returned where dropping privileges is not implemented.
var ErrUnsupported = errors.New("dropping privileges is not supported on this platform")

// SudoUser returns the account that ran sudo, when the process is root
// because of sudo. It returns false when not root, or root without sudo
// (a real root login or a container), where there is nobody to drop to.
func SudoUser() (*user.User, bool) {
	if os.Geteuid() != 0 {
		return nil, false
	}
	uid := os.Getenv("SUDO_UID")
	if uid == "" || uid == "0" {
		return nil, false
	}
	u, err := user.LookupId(uid)
	if err != nil {
		return nil, false
	}
	return u, true
}

// To switches the whole process to u: supplementary groups, then group,
// then user. It points HOME, USER and LOGNAME at u, so paths such as the
// state directory resolve to u's, and checks that root cannot be regained.
func To(u *user.User) error { return to(u) }
