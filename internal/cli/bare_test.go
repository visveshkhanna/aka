package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aka/internal/project"
)

func TestDevCommand(t *testing.T) {
	t.Setenv("AKA_STATE_DIR", t.TempDir())
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644)
	os.WriteFile(filepath.Join(root, "manage.py"), nil, 0o644)
	os.Mkdir(filepath.Join(root, ".git"), 0o755)
	sub := filepath.Join(root, "src")
	os.Mkdir(sub, 0o755)

	// Not a terminal: the first candidate, run from the project root.
	dir, cmd, err := devCommand(sub, false)
	if err != nil || dir != root || strings.Join(cmd, " ") != "npm run dev" {
		t.Fatalf("got %s %v %v", dir, cmd, err)
	}
	// A remembered choice wins, unless --choose.
	project.NewChoices(os.Getenv("AKA_STATE_DIR")).Set(root, []string{"python3", "manage.py", "runserver"})
	if _, cmd, _ := devCommand(sub, false); strings.Join(cmd, " ") != "python3 manage.py runserver" {
		t.Fatalf("saved choice ignored: %v", cmd)
	}
	if _, cmd, _ := devCommand(sub, true); strings.Join(cmd, " ") != "npm run dev" {
		t.Fatalf("--choose did not re-pick: %v", cmd)
	}

	empty := t.TempDir()
	os.Mkdir(filepath.Join(empty, ".git"), 0o755)
	if _, _, err := devCommand(empty, false); err == nil || !strings.Contains(err.Error(), "no dev command found") {
		t.Fatalf("empty project: %v", err)
	}
}
