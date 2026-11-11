package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func install(s Spec) (string, error) {
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	// Replace a loaded older definition.
	exec.Command("launchctl", "bootout", domain()+"/"+Label).Run()
	if err := writeFile(p, Plist(s)); err != nil {
		return "", err
	}
	if out, err := exec.Command("launchctl", "bootstrap", domain(), p).CombinedOutput(); err != nil {
		return p, fmt.Errorf("launchctl bootstrap: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return p, nil
}

func uninstall() error {
	exec.Command("launchctl", "bootout", domain()+"/"+Label).Run() // not loaded is fine
	if err := os.Remove(plistPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func query() Status {
	st := Status{Path: plistPath()}
	if _, err := os.Stat(st.Path); err == nil {
		st.Installed = true
	}
	out, err := exec.Command("launchctl", "print", domain()+"/"+Label).Output()
	if err == nil {
		st.Running = strings.Contains(string(out), "state = running")
		// The first state and pid lines are the job's own; nested
		// sections repeat "state =" for other things.
		var parts []string
		for _, prefix := range []string{"state = ", "pid = "} {
			for _, line := range strings.Split(string(out), "\n") {
				if t := strings.TrimSpace(line); strings.HasPrefix(t, prefix) {
					parts = append(parts, t)
					break
				}
			}
		}
		st.Detail = strings.Join(parts, ", ")
	}
	return st
}
