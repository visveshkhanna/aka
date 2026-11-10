//go:build !unix

package privdrop

import "os/user"

func to(u *user.User) error { return ErrUnsupported }
