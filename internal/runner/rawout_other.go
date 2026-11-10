//go:build unix && !(darwin || freebsd || netbsd || openbsd || dragonfly || linux)

package runner

import "os"

func rawOutput(*os.File) {}
