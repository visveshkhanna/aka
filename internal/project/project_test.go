package project

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tree writes files (path -> content) under a new temp dir.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestInferNameFromManifests(t *testing.T) {
	cases := []struct {
		file, content, want, source string
	}{
		{"package.json", `{"name":"@acme/Web_App"}`, "web-app", "package.json"},
		{"pyproject.toml", "[build-system]\nrequires = [\"x\"]\n[project]\nname = \"my_service\"\n", "my-service", "pyproject.toml"},
		{"pyproject.toml", "[tool.poetry]\nname = 'poetry-app'\nversion = \"1\"\n", "poetry-app", "pyproject.toml"},
		{"Cargo.toml", "[package]\nname = \"rusty\"\n[dependencies]\nname = \"not-this\"\n", "rusty", "Cargo.toml"},
		{"go.mod", "module github.com/acme/api/v2\n\ngo 1.22\n", "api", "go.mod"},
		{"composer.json", `{"name":"laravel/shop"}`, "shop", "composer.json"},
		{"pom.xml", "<project><parent><artifactId>spring-parent</artifactId></parent><artifactId>orders</artifactId></project>", "orders", "pom.xml"},
		{"settings.gradle.kts", `rootProject.name = "inventory"`, "inventory", "settings.gradle"},
		{"mix.exs", "def project do\n [app: :hello_phx, version: \"0.1.0\"]\nend", "hello-phx", "mix.exs"},
		{"Billing.csproj", "<Project/>", "billing", ".csproj"},
		{"angular.json", `{"projects":{"dashboard":{}}}`, "dashboard", "angular.json"},
	}
	for _, tc := range cases {
		root := tree(t, map[string]string{".git/HEAD": "", tc.file: tc.content})
		if got := InferName(root); got.Name != tc.want || got.Source != tc.source {
			t.Errorf("%s: got %+v, want %s from %s", tc.file, got, tc.want, tc.source)
		}
	}
}

func TestInferNameWalksUpToRepoRootOnly(t *testing.T) {
	root := tree(t, map[string]string{
		"package.json":             `{"name":"outside-repo"}`,
		"repo/.git/HEAD":           "",
		"repo/pyproject.toml":      "[project]\nname = \"inner\"\n",
		"repo/src/pkg/module/x.py": "",
		"other/.git/HEAD":          "",
		"other/deep/dir/file":      "",
		"plain/My Project/file":    "",
	})
	if got := InferName(filepath.Join(root, "repo/src/pkg/module")); got.Name != "inner" {
		t.Errorf("subdirectory: got %+v, want inner", got)
	}
	// The repo has no manifest: use its root's name, never the
	// package.json above the repo.
	if got := InferName(filepath.Join(root, "other/deep/dir")); got.Name != "other" || got.Source != "git root" {
		t.Errorf("no manifest: got %+v", got)
	}
	// Outside a repository the walk continues up (to below $HOME), so the
	// top-level package.json is this project's.
	if got := InferName(filepath.Join(root, "plain/My Project")); got.Name != "outside-repo" {
		t.Errorf("plain dir: got %+v", got)
	}
}

func TestSanitizeLabel(t *testing.T) {
	for in, want := range map[string]string{
		"MyApp": "myapp", "@scope/my_app": "my-app", "a--b..c": "a-b-c", "--x--": "x", "日本": "", "vendor/pkg": "pkg",
	} {
		if got := SanitizeLabel(in); got != want {
			t.Errorf("SanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", 80)
	a, b := SanitizeLabel(long), SanitizeLabel(long+"b")
	if len(a) > 63 || len(b) > 63 || a == b {
		t.Errorf("long labels %q %q must fit 63 bytes and stay distinct", a, b)
	}
}

func lookNone(string) bool { return false }

func TestDevCommands(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string // first candidate
		n     int
	}{
		{"npm dev", map[string]string{"package.json": `{"scripts":{"build":"x","dev":"vite","start":"node s"}}`}, []string{"npm", "run", "dev"}, 1},
		{"pnpm start", map[string]string{"package.json": `{"scripts":{"start":"ng serve"}}`, "pnpm-lock.yaml": ""}, []string{"pnpm", "run", "start"}, 1},
		{"packageManager field", map[string]string{"package.json": `{"packageManager":"yarn@4.1.0","scripts":{"dev":"next dev"}}`, "package-lock.json": ""}, []string{"yarn", "run", "dev"}, 1},
		{"bun", map[string]string{"package.json": `{"scripts":{"dev":"bun --hot x.ts"}}`, "bun.lock": ""}, []string{"bun", "run", "dev"}, 1},
		{"angular without scripts", map[string]string{"angular.json": "{}"}, []string{"npx", "ng", "serve"}, 1},
		{"django", map[string]string{"manage.py": ""}, []string{"python", "manage.py", "runserver"}, 1},
		{"django venv", map[string]string{"manage.py": "", ".venv/bin/python": ""}, []string{".venv/bin/python", "manage.py", "runserver"}, 1},
		{"procfile", map[string]string{"Procfile": "release: x\nweb: bundle exec puma -p $PORT\n"}, []string{"sh", "-c", "bundle exec puma -p $PORT"}, 1},
		{"rails bin/dev", map[string]string{"Gemfile": "", "bin/dev": "", "bin/rails": ""}, []string{"bin/dev"}, 1},
		{"rails", map[string]string{"Gemfile": "", "bin/rails": ""}, []string{"bin/rails", "server"}, 1},
		{"laravel", map[string]string{"artisan": "", "composer.json": "{}"}, []string{"php", "artisan", "serve"}, 1},
		{"phoenix", map[string]string{"mix.exs": "{:phoenix, \"~> 1.7\"}"}, []string{"mix", "phx.server"}, 1},
		{"spring maven wrapper", map[string]string{"pom.xml": "<artifactId>spring-boot-starter-web</artifactId>", "mvnw": ""}, []string{"./mvnw", "spring-boot:run"}, 1},
		{"spring gradle", map[string]string{"build.gradle.kts": `id("org.springframework.boot")`, "gradlew": ""}, []string{"./gradlew", "bootRun"}, 1},
		{"dotnet", map[string]string{"Api.csproj": ""}, []string{"dotnet", "watch", "run"}, 1},
		{"hugo", map[string]string{"hugo.toml": ""}, []string{"hugo", "server"}, 1},
		{"mkdocs", map[string]string{"mkdocs.yml": ""}, []string{"mkdocs", "serve"}, 1},
		{"cargo", map[string]string{"Cargo.toml": ""}, []string{"cargo", "run"}, 1},
		{"go main", map[string]string{"go.mod": "module x", "main.go": "package main\nfunc main() {}"}, []string{"go", "run", "."}, 1},
		{"go library", map[string]string{"go.mod": "module x", "lib.go": "package x"}, nil, 0},
		{"makefile", map[string]string{"Makefile": "build:\n\tgo build\nVAR := 1\ndev: build\n\t./x\n"}, []string{"make", "dev"}, 1},
		{"node and django", map[string]string{"package.json": `{"scripts":{"dev":"vite"}}`, "manage.py": ""}, []string{"npm", "run", "dev"}, 2},
		{"nothing", map[string]string{"README.md": ""}, nil, 0},
	}
	for _, tc := range cases {
		root := tree(t, tc.files)
		_, got := FindDevCommands(root, Env{LookPath: lookNone})
		if len(got) != tc.n {
			t.Errorf("%s: %d candidates %v, want %d", tc.name, len(got), got, tc.n)
			continue
		}
		if tc.n > 0 && !reflect.DeepEqual(got[0].Command, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got[0].Command, tc.want)
		}
	}
}

func TestDevCommandsPythonRunners(t *testing.T) {
	look := func(have ...string) Env {
		return Env{LookPath: func(c string) bool {
			for _, h := range have {
				if c == h {
					return true
				}
			}
			return false
		}}
	}
	root := tree(t, map[string]string{"manage.py": "", "uv.lock": ""})
	if _, c := FindDevCommands(root, look("uv", "python3")); c[0].String() != "uv run python manage.py runserver" {
		t.Errorf("uv: %v", c[0])
	}
	if _, c := FindDevCommands(root, look("python3")); c[0].String() != "python3 manage.py runserver" {
		t.Errorf("uv.lock without uv: %v", c[0])
	}
	root = tree(t, map[string]string{"manage.py": "", "poetry.lock": ""})
	if _, c := FindDevCommands(root, look("poetry")); c[0].String() != "poetry run python manage.py runserver" {
		t.Errorf("poetry: %v", c[0])
	}
}

func TestFindDevCommandsWalksUp(t *testing.T) {
	root := tree(t, map[string]string{".git/HEAD": "", "manage.py": "", "app/views/x.py": ""})
	dir, c := FindDevCommands(filepath.Join(root, "app/views"), Env{LookPath: lookNone})
	if dir != root || len(c) != 1 {
		t.Fatalf("got %s %v, want the repo root's manage.py", dir, c)
	}
}

func TestChoices(t *testing.T) {
	c := NewChoices(t.TempDir())
	if _, ok := c.Get("/p"); ok {
		t.Fatal("empty store returned a choice")
	}
	if err := c.Set("/p", []string{"npm", "run", "dev"}); err != nil {
		t.Fatal(err)
	}
	c.Set("/q", []string{"make", "dev"})
	if got, ok := c.Get("/p"); !ok || strings.Join(got, " ") != "npm run dev" {
		t.Fatalf("got %v %v", got, ok)
	}
	c.Set("/p", nil)
	if _, ok := c.Get("/p"); ok {
		t.Fatal("forgotten choice still returned")
	}
	if _, ok := c.Get("/q"); !ok {
		t.Fatal("other entry lost")
	}
}

func TestWalkUpWithoutRepoStopsBelowHome(t *testing.T) {
	home := tree(t, map[string]string{"package.json": `{"name":"home-junk","scripts":{"dev":"x"}}`, "proj/manage.py": "", "proj/app/x.py": "", "bare/sub/f": ""})
	t.Setenv("HOME", home)
	dir, c := FindDevCommands(filepath.Join(home, "proj/app"), Env{LookPath: lookNone})
	if dir != filepath.Join(home, "proj") || len(c) != 1 {
		t.Errorf("no repo: got %s %v, want proj's manage.py", dir, c)
	}
	if _, c := FindDevCommands(filepath.Join(home, "bare/sub"), Env{LookPath: lookNone}); len(c) != 0 {
		t.Errorf("walked into the home directory: %v", c)
	}
	if n := InferName(filepath.Join(home, "bare/sub")); n.Name != "sub" {
		t.Errorf("name taken from ~/package.json: %+v", n)
	}
}
