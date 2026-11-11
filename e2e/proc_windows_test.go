//go:build e2e && windows

package e2e

import "os/exec"

func setGroup(*exec.Cmd) {}

// interrupt kills aka: there is no Ctrl+C to send to a process on another
// console. Its job object must still take the app down.
func interrupt(cmd *exec.Cmd) { cmd.Process.Kill() }
