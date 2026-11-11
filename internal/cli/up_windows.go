package cli

import (
	"os"
	"os/exec"
	"syscall"
)

var upSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// upIsolate leaves the apps in our console: Ctrl+C reaches each directly.
func upIsolate(*exec.Cmd) {}

// upForward has nothing to do for Ctrl+C, which the console delivered.
// For a console close each app's aka handles its own event.
func upForward(*os.Process, os.Signal) {}
