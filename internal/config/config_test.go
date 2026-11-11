package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	p := write(t, `
# apps of the shop
[apps]
web = "npm run dev"   # the frontend
api = 'uvicorn main:app --reload'

[apps.worker]
cmd = "python -m worker --queue \"a#b\""
dir = "services/worker"
port_hint = 9_000
no_preset = true
`)
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(p)
	if len(f.Apps) != 3 {
		t.Fatalf("apps: %+v", f.Apps)
	}
	api, _ := f.App("api")
	web, _ := f.App("web")
	w, _ := f.App("worker")
	if api.Command != "uvicorn main:app --reload" || api.Dir != base {
		t.Errorf("api: %+v", api)
	}
	if web.Command != "npm run dev" {
		t.Errorf("web: %+v (comment not stripped?)", web)
	}
	if w.Command != `python -m worker --queue "a#b"` || w.Dir != filepath.Join(base, "services/worker") || w.PortHint != 9000 || !w.NoPreset {
		t.Errorf("worker: %+v", w)
	}
	if f.Apps[0].Name != "api" {
		t.Errorf("apps not sorted: %v", f.Apps)
	}
}

func TestLoadErrors(t *testing.T) {
	for content, want := range map[string]string{
		"[apps]\nweb = npm run dev":                     "unsupported value",
		"web = \"x\"":                                   "unknown setting",
		"[apps]\nweb = \"x\"\nweb = \"y\"":              "set twice",
		"[apps.web]\ncmd = \"x\"\nport_hint = \"8000\"": "port number",
		"[apps.web]\ncmd = \"x\"\ncolour = \"red\"":     "unknown app setting",
		"[apps.web]\ndir = \"x\"":                       "no command",
		"[[apps]]\nweb = \"x\"":                         "unsupported table",
		"[apps]\n":                                      "no apps",
		"[apps]\nweb = \"unterminated":                  "unterminated",
		"[apps]\nweb = 5":                               "must be a string",
	} {
		_, err := Load(write(t, content))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", content, err, want)
		}
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".git"), 0o755)
	sub := filepath.Join(root, "a", "b")
	os.MkdirAll(sub, 0o755)
	if Find(sub) != "" {
		t.Fatal("found a file that does not exist")
	}
	os.WriteFile(filepath.Join(root, FileName), []byte("[apps]\nx = \"y\"\n"), 0o644)
	if got := Find(sub); got != filepath.Join(root, FileName) {
		t.Fatalf("Find = %q", got)
	}
	// Never above the repository root.
	inner := filepath.Join(root, "a")
	os.Mkdir(filepath.Join(inner, ".git"), 0o755)
	if got := Find(sub); got != "" {
		t.Fatalf("crossed a repository root: %q", got)
	}
}
