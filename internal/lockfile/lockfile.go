// Package lockfile provides exclusive advisory file locks. On Unix they are
// flock(2) locks, which the kernel releases when the process dies, so there
// is never a stale lock to clean up.
package lockfile

import "errors"

// ErrLocked is returned by TryLock when another process holds the lock.
var ErrLocked = errors.New("lock held by another process")

// TryLock takes the lock at path without waiting. It returns ErrLocked if
// another process holds it. Call the returned function to release it.
func TryLock(path string) (unlock func(), err error) { return lock(path, false) }

// Lock takes the lock at path, waiting for any other holder to release it.
func Lock(path string) (unlock func(), err error) { return lock(path, true) }
