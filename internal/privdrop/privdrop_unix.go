//go:build unix

package privdrop

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func to(u *user.User) error {
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	groups := []int{gid}
	if ids, err := u.GroupIds(); err == nil {
		for _, s := range ids {
			if g, err := strconv.Atoi(s); err == nil && g != gid {
				groups = append(groups, g)
			}
		}
	}
	// Order matters: once the uid changes we can no longer change groups.
	// Since Go 1.16 these apply to every thread of the process.
	if err := syscall.Setgroups(groups); err != nil {
		return fmt.Errorf("setgroups: %w", err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("setgid: %w", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid: %w", err)
	}
	if os.Geteuid() != uid || os.Getuid() != uid || os.Getegid() != gid {
		return errors.New("privileges were not fully dropped")
	}
	if uid != 0 && syscall.Setuid(0) == nil {
		return errors.New("root could be regained after dropping privileges")
	}
	os.Setenv("HOME", u.HomeDir)
	os.Setenv("USER", u.Username)
	os.Setenv("LOGNAME", u.Username)
	for _, k := range []string{"SUDO_UID", "SUDO_GID", "SUDO_USER", "SUDO_COMMAND"} {
		os.Unsetenv(k)
	}
	return nil
}
