package project

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Candidate is a command that probably starts the project's dev server.
type Candidate struct {
	Command []string
	Source  string // why we think so, e.g. `package.json "dev" script`
}

// String is the command as typed.
func (c Candidate) String() string { return strings.Join(c.Command, " ") }

// Env answers questions about the machine, replaceable in tests.
type Env struct {
	// LookPath reports whether a command is installed.
	LookPath func(string) bool
}

// FindDevCommands looks for dev-server commands from dir up to the
// repository root and returns the directory where the first ones were found
// (where the command must run) and the candidates there, best first.
func FindDevCommands(dir string, env Env) (string, []Candidate) {
	dir, _ = filepath.Abs(dir)
	found, cands := dir, []Candidate(nil)
	walkUp(dir, GitRoot(dir), func(d string) bool {
		if c := devCommands(d, env); len(c) > 0 {
			found, cands = d, c
			return true
		}
		return false
	})
	return found, cands
}

func devCommands(d string, env Env) []Candidate {
	var out []Candidate
	add := func(src string, cmd ...string) { out = append(out, Candidate{cmd, src}) }
	has := func(name string) bool { _, err := os.Stat(filepath.Join(d, name)); return err == nil }

	// Node: the package.json script, run with the project's package manager.
	if scripts := packageScripts(d); scripts != nil {
		pm := PackageManager(d)
		for _, s := range []string{"dev", "serve", "start"} {
			if _, ok := scripts[s]; ok {
				add(`package.json "`+s+`" script`, pm, "run", s)
				break // one candidate per manifest: the most dev-like script
			}
		}
	} else if has("angular.json") {
		add("angular.json", "npx", "ng", "serve")
	}

	// Python: Django.
	if has("manage.py") {
		add("manage.py", append(pythonRunner(d, env), "manage.py", "runserver")...)
	}

	// Procfile (Heroku, foreman): the web process.
	if web := procfileWeb(filepath.Join(d, "Procfile.dev")); web != "" {
		add("Procfile.dev web", "sh", "-c", web)
	} else if web := procfileWeb(filepath.Join(d, "Procfile")); web != "" {
		add("Procfile web", "sh", "-c", web)
	}

	// Ruby on Rails.
	if has("Gemfile") {
		switch {
		case has("bin/dev"):
			add("bin/dev", "bin/dev")
		case has("bin/rails"):
			add("bin/rails", "bin/rails", "server")
		}
	}

	// PHP: Laravel.
	if has("artisan") && has("composer.json") {
		add("artisan", "php", "artisan", "serve")
	}

	// Elixir: Phoenix.
	if has("mix.exs") && fileContains(filepath.Join(d, "mix.exs"), ":phoenix") {
		add("mix.exs (Phoenix)", "mix", "phx.server")
	}

	// JVM: Spring Boot.
	if has("pom.xml") && fileContains(filepath.Join(d, "pom.xml"), "spring-boot") {
		if has("mvnw") {
			add("pom.xml (Spring Boot)", "./mvnw", "spring-boot:run")
		} else {
			add("pom.xml (Spring Boot)", "mvn", "spring-boot:run")
		}
	}
	for _, g := range []string{"build.gradle", "build.gradle.kts"} {
		if has(g) && fileContains(filepath.Join(d, g), "org.springframework.boot") {
			if has("gradlew") {
				add(g+" (Spring Boot)", "./gradlew", "bootRun")
			} else {
				add(g+" (Spring Boot)", "gradle", "bootRun")
			}
			break
		}
	}

	// .NET.
	if m, _ := filepath.Glob(filepath.Join(d, "*.csproj")); len(m) > 0 {
		add("*.csproj", "dotnet", "watch", "run")
	}

	// Static site generators.
	for _, f := range []string{"hugo.toml", "hugo.yaml", "hugo.json"} {
		if has(f) {
			add(f, "hugo", "server")
			break
		}
	}
	if has("mkdocs.yml") || has("mkdocs.yaml") {
		add("mkdocs.yml", "mkdocs", "serve")
	}

	// Compiled languages: run the program and let it read PORT.
	if has("Cargo.toml") {
		add("Cargo.toml", "cargo", "run")
	}
	if has("go.mod") && hasGoMain(d) {
		add("go.mod", "go", "run", ".")
	}

	// A Makefile with a dev or run target, as a last resort.
	if t := makeTarget(d, "dev", "run", "serve"); t != "" {
		add("Makefile "+t+" target", "make", t)
	}
	return out
}

// PackageManager picks the package manager for a Node project: the
// packageManager field, then a lock file, from dir up; npm otherwise.
func PackageManager(dir string) string {
	locks := []struct{ file, pm string }{
		{"pnpm-lock.yaml", "pnpm"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"yarn.lock", "yarn"}, {"package-lock.json", "npm"},
	}
	for d := dir; ; {
		var p struct {
			PackageManager string `json:"packageManager"`
		}
		readJSON(filepath.Join(d, "package.json"), &p)
		if name, _, _ := strings.Cut(p.PackageManager, "@"); name == "pnpm" || name == "yarn" || name == "bun" || name == "npm" {
			return name
		}
		for _, l := range locks {
			if _, err := os.Stat(filepath.Join(d, l.file)); err == nil {
				return l.pm
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "npm"
		}
		d = parent
	}
}

func packageScripts(d string) map[string]string {
	b, err := os.ReadFile(filepath.Join(d, "package.json"))
	if err != nil {
		return nil
	}
	var p struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(b, &p) != nil || p.Scripts == nil {
		return nil
	}
	return p.Scripts
}

// pythonRunner is how to run Python in this project: through uv or poetry
// when the project uses them, the project's virtualenv, or python3.
func pythonRunner(d string, env Env) []string {
	has := func(name string) bool { _, err := os.Stat(filepath.Join(d, name)); return err == nil }
	switch {
	case has("uv.lock") && env.LookPath("uv"):
		return []string{"uv", "run", "python"}
	case has("poetry.lock") && env.LookPath("poetry"):
		return []string{"poetry", "run", "python"}
	}
	for _, venv := range []string{".venv", "venv"} {
		for _, p := range []string{filepath.Join(venv, "bin", "python"), filepath.Join(venv, "Scripts", "python.exe")} {
			if has(p) {
				return []string{p}
			}
		}
	}
	if runtime.GOOS == "windows" && env.LookPath("py") {
		return []string{"py"} // the Python launcher
	}
	if env.LookPath("python3") {
		return []string{"python3"}
	}
	return []string{"python"}
}

var procWeb = regexp.MustCompile(`^web\s*:\s*(.+)$`)

func procfileWeb(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := procWeb.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func fileContains(path, s string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), s)
}

// hasGoMain reports whether the directory's own package is main.
func hasGoMain(d string) bool {
	m, _ := filepath.Glob(filepath.Join(d, "*.go"))
	for _, f := range m {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if fileContains(f, "package main") && fileContains(f, "func main(") {
			return true
		}
	}
	return false
}

var makeRule = regexp.MustCompile(`^([A-Za-z0-9_.\-]+)\s*:([^=]|$)`)

// makeTarget returns the first of names that the Makefile defines.
func makeTarget(d string, names ...string) string {
	b, err := os.ReadFile(filepath.Join(d, "Makefile"))
	if err != nil {
		return ""
	}
	defined := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if m := makeRule.FindStringSubmatch(line); m != nil {
			defined[m[1]] = true
		}
	}
	for _, n := range names {
		if defined[n] {
			return n
		}
	}
	return ""
}
