package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"aka/internal/brand"
)

// Worktree is a hostname prefix for a linked git worktree.
type Worktree struct {
	Prefix string
	Source string
}

// defaultBranches get no prefix: they are the primary checkout.
var defaultBranches = map[string]bool{"main": true, "master": true, "HEAD": true, "": true}

// WorktreePrefix returns a prefix when dir is inside a linked git worktree
// (made with `git worktree add`) on a non-default branch, so that checkouts
// of one project get distinct names: feature-x.myapp.localhost. The main
// checkout never gets one, even on a feature branch. AKA_PREFIX overrides
// detection; set it empty to turn the prefix off.
func WorktreePrefix(dir string) (Worktree, bool) {
	if v, ok := os.LookupEnv(brand.Env("PREFIX")); ok {
		if p := SanitizeLabel(v); p != "" {
			return Worktree{p, brand.Env("PREFIX")}, true
		}
		return Worktree{}, false
	}
	if w, ok, known := worktreeViaGit(dir); known {
		return w, ok
	}
	return worktreeViaFiles(dir)
}

// branchPrefix uses the branch's last segment: feature/login -> login.
func branchPrefix(branch string) (Worktree, bool) {
	if defaultBranches[branch] {
		return Worktree{}, false
	}
	p := SanitizeLabel(branch[strings.LastIndex(branch, "/")+1:])
	return Worktree{p, "git branch " + branch}, p != ""
}

// worktreeViaGit asks git. known is false when git is unavailable or dir is
// not in a repository, so the caller can fall back to reading files.
func worktreeViaGit(dir string) (w Worktree, ok, known bool) {
	git := func(args ...string) (string, bool) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		done := make(chan struct{})
		var out []byte
		var err error
		go func() { out, err = cmd.Output(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			<-done
			return "", false
		}
		return strings.TrimSpace(string(out)), err == nil
	}
	gitDir, ok1 := git("rev-parse", "--absolute-git-dir")
	common, ok2 := git("rev-parse", "--git-common-dir")
	if !ok1 || !ok2 {
		return Worktree{}, false, false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	if sameDir(gitDir, common) {
		return Worktree{}, false, true // the main checkout
	}
	branch, _ := git("rev-parse", "--abbrev-ref", "HEAD")
	w, ok = branchPrefix(branch)
	return w, ok, true
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

var (
	gitdirLine = regexp.MustCompile(`^gitdir:\s*(.+)$`)
	headRef    = regexp.MustCompile(`^ref: refs/heads/(.+)$`)
	worktreeIn = regexp.MustCompile(`[/\\]worktrees[/\\][^/\\]+$`)
)

// worktreeViaFiles reads .git without git: a linked worktree has a .git
// file pointing into <repo>/.git/worktrees/<name> (a submodule's points
// into .git/modules and is not a worktree).
func worktreeViaFiles(dir string) (Worktree, bool) {
	for d := dir; ; {
		p := filepath.Join(d, ".git")
		if st, err := os.Stat(p); err == nil {
			if st.IsDir() {
				return Worktree{}, false
			}
			b, _ := os.ReadFile(p)
			m := gitdirLine.FindStringSubmatch(strings.TrimSpace(string(b)))
			if m == nil || !worktreeIn.MatchString(m[1]) {
				return Worktree{}, false
			}
			gd := m[1]
			if !filepath.IsAbs(gd) {
				gd = filepath.Join(d, gd)
			}
			head, _ := os.ReadFile(filepath.Join(gd, "HEAD"))
			hm := headRef.FindStringSubmatch(strings.TrimSpace(string(head)))
			if hm == nil {
				return Worktree{}, false // detached HEAD
			}
			return branchPrefix(hm[1])
		}
		parent := filepath.Dir(d)
		if parent == d {
			return Worktree{}, false
		}
		d = parent
	}
}
