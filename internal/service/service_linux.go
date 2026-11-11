package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unitPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", unitName)
}

func systemctl(args ...string) (string, error) {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func install(s Spec) (string, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "", fmt.Errorf("systemctl not found: %w", ErrUnsupported)
	}
	p := unitPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := writeFile(p, Unit(s)); err != nil {
		return "", err
	}
	if out, err := systemctl("daemon-reload"); err != nil {
		return p, fmt.Errorf("systemctl --user daemon-reload: %v: %s", err, out)
	}
	if out, err := systemctl("enable", "--now", unitName); err != nil {
		return p, fmt.Errorf("systemctl --user enable --now: %v: %s", err, out)
	}
	return p, nil
}

func uninstall() error {
	systemctl("disable", "--now", unitName) // not installed is fine
	if err := os.Remove(unitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	systemctl("daemon-reload")
	return nil
}

func query() Status {
	st := Status{Path: unitPath()}
	if _, err := os.Stat(st.Path); err == nil {
		st.Installed = true
	}
	out, _ := systemctl("is-active", unitName)
	st.Running = out == "active"
	st.Detail = out
	return st
}

const startsOnInstall = true
