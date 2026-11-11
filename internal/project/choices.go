package project

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Choices remembers which dev command the user picked for a directory when
// several were possible. It lives in aka's state directory, not in the
// project, so nothing is written into the user's repository.
type Choices struct{ path string }

// NewChoices returns the store in stateDir.
func NewChoices(stateDir string) Choices { return Choices{filepath.Join(stateDir, "projects.json")} }

func (c Choices) load() map[string][]string {
	m := map[string][]string{}
	if b, err := os.ReadFile(c.path); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

// Get returns the saved command for dir, if any.
func (c Choices) Get(dir string) ([]string, bool) {
	cmd, ok := c.load()[dir]
	return cmd, ok && len(cmd) > 0
}

// Set saves cmd for dir (nil forgets it).
func (c Choices) Set(dir string, cmd []string) error {
	m := c.load()
	if cmd == nil {
		delete(m, dir)
	} else {
		m[dir] = cmd
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
