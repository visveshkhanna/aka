// Package project works out facts about the project in a directory: its
// name (for the hostname) and the command that starts its dev server (for
// a bare `aka`), across ecosystems.
package project

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Name is an inferred app name and where it came from.
type Name struct {
	Name   string
	Source string // e.g. "package.json", "git root", "directory name"
}

// manifest reads a project name from one kind of file in a directory.
type manifest struct {
	source string
	read   func(dir string) string
}

// manifests are checked in each directory, nearest directory first.
var manifests = []manifest{
	{"package.json", func(d string) string {
		var p struct {
			Name string `json:"name"`
		}
		readJSON(filepath.Join(d, "package.json"), &p)
		return p.Name
	}},
	{"pyproject.toml", func(d string) string {
		t := readTOMLStrings(filepath.Join(d, "pyproject.toml"))
		if n := t["project.name"]; n != "" {
			return n
		}
		return t["tool.poetry.name"]
	}},
	{"Cargo.toml", func(d string) string { return readTOMLStrings(filepath.Join(d, "Cargo.toml"))["package.name"] }},
	{"go.mod", func(d string) string { return goModuleName(filepath.Join(d, "go.mod")) }},
	{"composer.json", func(d string) string {
		var p struct {
			Name string `json:"name"`
		}
		readJSON(filepath.Join(d, "composer.json"), &p)
		return p.Name
	}},
	{"pom.xml", func(d string) string { return pomArtifactID(filepath.Join(d, "pom.xml")) }},
	{"settings.gradle", func(d string) string {
		for _, f := range []string{"settings.gradle", "settings.gradle.kts"} {
			if n := gradleRootName(filepath.Join(d, f)); n != "" {
				return n
			}
		}
		return ""
	}},
	{"mix.exs", func(d string) string { return mixAppName(filepath.Join(d, "mix.exs")) }},
	{".csproj", func(d string) string {
		for _, pat := range []string{"*.csproj", "*.fsproj", "*.vbproj"} {
			if m, _ := filepath.Glob(filepath.Join(d, pat)); len(m) == 1 {
				return strings.TrimSuffix(filepath.Base(m[0]), filepath.Ext(m[0]))
			}
		}
		return ""
	}},
	{"angular.json", func(d string) string {
		var a struct {
			DefaultProject string                     `json:"defaultProject"`
			Projects       map[string]json.RawMessage `json:"projects"`
		}
		readJSON(filepath.Join(d, "angular.json"), &a)
		if a.DefaultProject != "" {
			return a.DefaultProject
		}
		if len(a.Projects) == 1 {
			for k := range a.Projects {
				return k
			}
		}
		return ""
	}},
}

// InferName works out an app name for dir. It walks up from dir to the
// repository root (never above it, so a stray manifest in a parent
// directory is not picked up), taking the first manifest name found, then
// falls back to the repository root's name, then dir's own name.
func InferName(dir string) Name {
	dir, _ = filepath.Abs(dir)
	root := GitRoot(dir)
	var found Name
	walkUp(dir, root, func(d string) bool {
		for _, m := range manifests {
			if n := SanitizeLabel(m.read(d)); n != "" {
				found = Name{n, m.source}
				return true
			}
		}
		return false
	})
	if found.Name != "" {
		return found
	}
	if root != "" {
		if n := SanitizeLabel(filepath.Base(root)); n != "" {
			return Name{n, "git root"}
		}
	}
	if n := SanitizeLabel(filepath.Base(dir)); n != "" {
		return Name{n, "directory name"}
	}
	return Name{"app", "default"}
}

// walkUp calls fn on dir and its parents until fn returns true. It stops at
// the repository root when there is one. Outside a repository it stops
// below the home directory (so ~/package.json is never taken for the
// project) or at the filesystem root.
func walkUp(dir, gitRoot string, fn func(string) bool) {
	home, _ := os.UserHomeDir()
	for d := dir; ; {
		if fn(d) || d == gitRoot {
			return
		}
		parent := filepath.Dir(d)
		if parent == d || gitRoot == "" && parent == home {
			return
		}
		d = parent
	}
}

// GitRoot returns the nearest directory at or above dir containing .git (a
// directory, or a file for worktrees and submodules), or "".
func GitRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

const maxLabel = 63

// SanitizeLabel turns an arbitrary name (a directory, @scope/pkg,
// vendor/pkg, a Go module path) into one DNS label: the last path segment,
// lowercased, with every run of other characters turned into one '-'. A
// label over 63 bytes is cut and given a short hash of the full name, so
// distinct long names stay distinct.
func SanitizeLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	var b strings.Builder
	dash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) <= maxLabel {
		return out
	}
	sum := sha256.Sum256([]byte(out))
	return strings.TrimRight(out[:maxLabel-7], "-") + "-" + hex.EncodeToString(sum[:])[:6]
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, v)
	}
}

var (
	tomlSection = regexp.MustCompile(`^\[\s*([A-Za-z0-9_.\-]+)\s*\]`)
	tomlString  = regexp.MustCompile(`^([A-Za-z0-9_\-]+)\s*=\s*["']([^"']*)["']`)
)

// readTOMLStrings is a minimal TOML reader: it maps "section.key" to the
// value of simple one-line string assignments, which is all name inference
// needs. Tables of arrays and inline tables are ignored.
func readTOMLStrings(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[[") {
			section = "[[array]]"
			continue
		}
		if m := tomlSection.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		if m := tomlString.FindStringSubmatch(line); m != nil {
			key := m[1]
			if section != "" {
				key = section + "." + key
			}
			if _, dup := out[key]; !dup {
				out[key] = m[2]
			}
		}
	}
	return out
}

var goMajor = regexp.MustCompile(`^v[0-9]+$`)

// goModuleName is the last meaningful segment of the module path:
// github.com/acme/api/v2 -> api.
func goModuleName(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "module" {
			segs := strings.Split(strings.Trim(f[1], `"`), "/")
			for i := len(segs) - 1; i >= 0; i-- {
				if !goMajor.MatchString(segs[i]) {
					return segs[i]
				}
			}
		}
	}
	return ""
}

// pomArtifactID is the project's own artifactId, not its parent's.
func pomArtifactID(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pom struct {
		ArtifactID string `xml:"artifactId"`
	}
	if xml.Unmarshal(b, &pom) != nil {
		return ""
	}
	return pom.ArtifactID
}

var gradleRoot = regexp.MustCompile(`rootProject\.name\s*=\s*["']([^"']+)["']`)

func gradleRootName(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := gradleRoot.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

var mixApp = regexp.MustCompile(`app:\s*:([a-z0-9_]+)`)

func mixAppName(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := mixApp.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}
