package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWorktreePrefix(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("AKA_PREFIX", "") // make sure the env override is not what we test
	os.Unsetenv("AKA_PREFIX")
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0o755)
	run(t, repo, "init", "-q", "-b", "main")
	run(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "wt")
	run(t, repo, "worktree", "add", "-q", "-b", "feature/Login_Page", wt)
	os.MkdirAll(filepath.Join(wt, "src"), 0o755)

	if w, ok := WorktreePrefix(repo); ok {
		t.Errorf("main checkout got prefix %+v", w)
	}
	// A feature branch in the main checkout gets no prefix either.
	run(t, repo, "checkout", "-q", "-b", "other")
	if w, ok := WorktreePrefix(repo); ok {
		t.Errorf("main checkout on a branch got prefix %+v", w)
	}
	for _, d := range []string{wt, filepath.Join(wt, "src")} {
		if w, ok := WorktreePrefix(d); !ok || w.Prefix != "login-page" {
			t.Errorf("%s: got %+v %v, want login-page", d, w, ok)
		}
	}
	// Without git, the .git file is read directly.
	if w, ok := worktreeViaFiles(filepath.Join(wt, "src")); !ok || w.Prefix != "login-page" {
		t.Errorf("file fallback: got %+v %v", w, ok)
	}
	if _, ok := worktreeViaFiles(repo); ok {
		t.Error("file fallback gave the main checkout a prefix")
	}
	// A worktree on main gets none.
	wt2 := filepath.Join(root, "wt2")
	run(t, repo, "worktree", "add", "-q", "--detach", wt2)
	if w, ok := WorktreePrefix(wt2); ok {
		t.Errorf("detached worktree got %+v", w)
	}

	t.Setenv("AKA_PREFIX", "Staging")
	if w, ok := WorktreePrefix(repo); !ok || w.Prefix != "staging" {
		t.Errorf("AKA_PREFIX: got %+v %v", w, ok)
	}
	t.Setenv("AKA_PREFIX", "")
	if _, ok := WorktreePrefix(wt); ok {
		t.Error("empty AKA_PREFIX did not turn the prefix off")
	}
}

func TestSubmoduleIsNotAWorktree(t *testing.T) {
	d := tree(t, map[string]string{".git": "gitdir: ../.git/modules/lib\n"})
	if _, ok := worktreeViaFiles(d); ok {
		t.Error("submodule treated as a worktree")
	}
}
