package presets

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const port = 4123

func noEnv(string) string { return "" }

func split(s string) []string { return strings.Fields(s) }

func TestBuiltinParses(t *testing.T) {
	s := Builtin()
	if len(s.Names()) < 25 {
		t.Fatalf("only %d presets", len(s.Names()))
	}
	seen := map[string]bool{}
	for _, n := range s.Names() {
		if seen[n] {
			t.Errorf("duplicate preset %q", n)
		}
		seen[n] = true
	}
}

// TestCommands covers every built-in preset, the wrappers,
// and the cases that must be left alone.
func TestCommands(t *testing.T) {
	s := Builtin()
	cases := []struct {
		in, want, preset string
	}{
		// Angular
		{"ng serve", "ng serve --port 4123 --host 127.0.0.1", "angular"},
		{"ng s --open", "ng s --open --port 4123 --host 127.0.0.1", "angular"},
		{"ng build", "ng build", "angular"},
		{"ng test", "ng test", "angular"},
		{"ng serve --port 4300", "ng serve --port 4300 --host 127.0.0.1", "angular"},
		{"ng serve --port=4300 --host=0.0.0.0", "ng serve --port=4300 --host=0.0.0.0", "angular"},
		{"npx ng serve", "npx ng serve --port 4123 --host 127.0.0.1", "angular"},
		{"/repo/node_modules/.bin/ng serve", "/repo/node_modules/.bin/ng serve --port 4123 --host 127.0.0.1", "angular"},

		// Vite family
		{"vite", "vite --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"vite dev", "vite dev --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"vite ./site", "vite ./site --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"vite build", "vite build", "vite"},
		{"vite --mode dev build", "vite --mode dev build", "vite"},
		{"vite --mode staging", "vite --mode staging --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"vite dev -- extra", "vite dev --port 4123 --strictPort --host 127.0.0.1 -- extra", "vite"},
		{"bunx --bun vite", "bunx --bun vite --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"pnpm exec vite", "pnpm exec vite --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"yarn dlx vite dev", "yarn dlx vite dev --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"npm exec vite", "npm exec vite -- --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"npm exec -- vite", "npm exec -- vite --port 4123 --strictPort --host 127.0.0.1", "vite"},
		{"astro dev", "astro dev --port 4123 --host 127.0.0.1", "astro"},
		{"astro build", "astro build", "astro"},
		{"astro", "astro", "astro"},
		{"vitepress dev docs", "vitepress dev docs --port 4123 --strictPort --host 127.0.0.1", "vitepress"},
		{"wrangler dev", "wrangler dev --port 4123 --ip 127.0.0.1", "wrangler"},
		{"expo start --lan", "expo start --lan --port 4123", "expo"},

		// Django
		{"python manage.py runserver", "python manage.py runserver 127.0.0.1:4123", "django"},
		{"python3 -u manage.py runserver --noreload", "python3 -u manage.py runserver 127.0.0.1:4123 --noreload", "django"},
		{"./manage.py runserver", "./manage.py runserver 127.0.0.1:4123", "django"},
		{"python manage.py runserver 8001", "python manage.py runserver 8001", "django"},
		{"python manage.py migrate", "python manage.py migrate", "django"},
		{"python manage.py --settings x.dev runserver", "python manage.py --settings x.dev runserver 127.0.0.1:4123", "django"},
		{"python -m django runserver", "python -m django runserver 127.0.0.1:4123", "django"},
		{"poetry run python manage.py runserver", "poetry run python manage.py runserver 127.0.0.1:4123", "django"},
		{"uv run --with django python manage.py runserver", "uv run --with django python manage.py runserver 127.0.0.1:4123", "django"},
		{"pipenv run ./manage.py runserver", "pipenv run ./manage.py runserver 127.0.0.1:4123", "django"},

		// Python servers
		{"uvicorn main:app --reload", "uvicorn main:app --reload --port 4123 --host 127.0.0.1", "uvicorn"},
		{"uvicorn main:app --port 9000", "uvicorn main:app --port 9000 --host 127.0.0.1", "uvicorn"},
		{"uvicorn main:app --uds /tmp/s", "uvicorn main:app --uds /tmp/s", "uvicorn"},
		{"python -m uvicorn main:app", "python -m uvicorn main:app --port 4123 --host 127.0.0.1", "uvicorn"},
		{"uv run uvicorn main:app", "uv run uvicorn main:app --port 4123 --host 127.0.0.1", "uvicorn"},
		{"fastapi dev main.py", "fastapi dev main.py --port 4123 --host 127.0.0.1", "fastapi"},
		{"gunicorn app:app -w 2", "gunicorn app:app -w 2 -b 127.0.0.1:4123", "gunicorn"},
		{"gunicorn --bind :8000 app:app", "gunicorn --bind :8000 app:app", "gunicorn"},
		{"jupyter lab", "jupyter lab --port=4123 --no-browser", "jupyter"},
		{"jupyter-lab", "jupyter-lab --port=4123 --no-browser", "jupyter"},
		{"jupyter nbconvert x.ipynb", "jupyter nbconvert x.ipynb", "jupyter"},
		{"mkdocs serve", "mkdocs serve -a 127.0.0.1:4123", "mkdocs"},
		{"mkdocs build", "mkdocs build", "mkdocs"},
		{"python -m http.server", "python -m http.server 4123 --bind 127.0.0.1", "http.server"},
		{"python3 -m http.server -d site", "python3 -m http.server 4123 -d site --bind 127.0.0.1", "http.server"},
		{"python -m http.server 9000", "python -m http.server 9000 --bind 127.0.0.1", "http.server"},
		{"python -mhttp.server", "python -mhttp.server 4123 --bind 127.0.0.1", "http.server"},

		// Ruby
		{"rails s", "rails s -p 4123 -b 127.0.0.1", "rails"},
		{"bin/rails server", "bin/rails server -p 4123 -b 127.0.0.1", "rails"},
		{"bundle exec rails server -p 3001", "bundle exec rails server -p 3001 -b 127.0.0.1", "rails"},
		{"rails console", "rails console", "rails"},
		{"bundle exec jekyll serve", "bundle exec jekyll serve --port 4123 --host 127.0.0.1", "jekyll"},

		// PHP
		{"php -S", "php -S 127.0.0.1:4123", "php"},
		{"php -S -t public", "php -S 127.0.0.1:4123 -t public", "php"},
		{"php -S localhost:8000", "php -S localhost:8000", "php"},
		{"php artisan serve", "php artisan serve --port=4123 --host=127.0.0.1", "laravel"},
		{"php artisan migrate", "php artisan migrate", "laravel"},

		// Static sites
		{"hugo server -D", "hugo server -D --port 4123 --bind 127.0.0.1", "hugo"},
		{"hugo", "hugo", "hugo"},

		// Not ours
		{"node server.js", "node server.js", ""},
		{"next dev", "next dev", ""},
		{"pnpm install", "pnpm install", ""},
		{"cargo run", "cargo run", ""},
	}
	for _, tc := range cases {
		r := s.Apply(split(tc.in), port, t.TempDir(), noEnv)
		if got := strings.Join(r.Args, " "); got != tc.want || r.Preset != tc.preset {
			t.Errorf("%s\n  got  %q (preset %q)\n  want %q (preset %q)", tc.in, got, r.Preset, tc.want, tc.preset)
		}
	}
}

func TestEnvPresets(t *testing.T) {
	s := Builtin()
	cases := []struct {
		in, preset string
		env        []string
	}{
		{"flask run", "flask", []string{"FLASK_RUN_HOST=127.0.0.1", "FLASK_RUN_PORT=4123"}},
		{"python -m flask run --debug", "flask", []string{"FLASK_RUN_HOST=127.0.0.1", "FLASK_RUN_PORT=4123"}},
		{"streamlit run app.py", "streamlit", []string{"STREAMLIT_SERVER_ADDRESS=127.0.0.1", "STREAMLIT_SERVER_HEADLESS=true", "STREAMLIT_SERVER_PORT=4123"}},
		{"python app.py", "gradio", []string{"GRADIO_SERVER_NAME=127.0.0.1", "GRADIO_SERVER_PORT=4123"}},
		{"mvn spring-boot:run", "spring-boot", []string{"SERVER_ADDRESS=127.0.0.1", "SERVER_PORT=4123"}},
		{"./gradlew bootRun", "spring-boot", []string{"SERVER_ADDRESS=127.0.0.1", "SERVER_PORT=4123"}},
		{"java -jar app.jar", "spring-boot", []string{"SERVER_ADDRESS=127.0.0.1", "SERVER_PORT=4123"}},
		{"quarkus dev", "quarkus", []string{"QUARKUS_HTTP_HOST=127.0.0.1", "QUARKUS_HTTP_PORT=4123"}},
		{"dotnet watch run", "aspnet", []string{"ASPNETCORE_URLS=http://127.0.0.1:4123"}},
	}
	for _, tc := range cases {
		r := s.Apply(split(tc.in), port, t.TempDir(), noEnv)
		if r.Preset != tc.preset || !reflect.DeepEqual(r.Env, tc.env) {
			t.Errorf("%s: preset %q env %v, want %q %v", tc.in, r.Preset, r.Env, tc.preset, tc.env)
		}
		if strings.Join(r.Args, " ") != tc.in {
			t.Errorf("%s: args changed to %v", tc.in, r.Args)
		}
	}
	// The user's own setting wins.
	r := s.Apply(split("flask run"), port, t.TempDir(), func(k string) string {
		if k == "FLASK_RUN_PORT" {
			return "5000"
		}
		return ""
	})
	if !reflect.DeepEqual(r.Env, []string{"FLASK_RUN_HOST=127.0.0.1"}) {
		t.Errorf("user FLASK_RUN_PORT not respected: %v", r.Env)
	}
}

func writePkg(t *testing.T, scripts string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{`+scripts+`}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPackageScripts(t *testing.T) {
	s := Builtin()
	dir := writePkg(t, `
		"dev": "vite",
		"ng": "ng serve --open",
		"env": "NODE_ENV=development vite dev",
		"both": "tsc -w & vite",
		"bg": "vite & tsc -w",
		"chain": "npm run build && vite preview",
		"comment": "vite # local only",
		"redir": "vite 2>&1",
		"quoted": "vite --base '/a&b/'",
		"fixed": "vite --port 5000",
		"build": "vite build",
		"next": "next dev",
		"sep": "vite -- x",
		"dj": "python manage.py runserver"`)
	cases := []struct {
		in, want, preset string
		skipped          bool
	}{
		{"npm run dev", "npm run dev -- --port 4123 --strictPort --host 127.0.0.1", "vite", false},
		{"npm run dev -- --open", "npm run dev -- --open --port 4123 --strictPort --host 127.0.0.1", "vite", false},
		{"pnpm run dev", "pnpm run dev --port 4123 --strictPort --host 127.0.0.1", "vite", false},
		{"yarn run ng", "yarn run ng --port 4123 --host 127.0.0.1", "angular", false},
		{"bun run env", "bun run env --port 4123 --strictPort --host 127.0.0.1", "vite", false},
		{"npm run redir", "npm run redir -- --port 4123 --strictPort --host 127.0.0.1", "vite", false},
		{"npm run quoted", "npm run quoted -- --port 4123 --strictPort --host 127.0.0.1", "vite", false},
		{"npm run fixed", "npm run fixed -- --host 127.0.0.1", "vite", false},
		{"npm run dev -- --port 6000", "npm run dev -- --port 6000 --host 127.0.0.1", "vite", false},
		{"npm run both", "npm run both", "", false}, // starts with tsc
		{"npm run comment", "npm run comment", "vite", true},
		{"npm run bg", "npm run bg", "vite", true},
		{"npm run sep", "npm run sep", "vite", true},
		{"npm run build", "npm run build", "vite", true},
		{"npm run dj", "npm run dj -- 127.0.0.1:4123", "django", false},
		{"npm run next", "npm run next", "", false},
		{"npm run missing", "npm run missing", "", false},
	}
	for _, tc := range cases {
		r := s.Apply(split(tc.in), port, dir, noEnv)
		got := strings.Join(r.Args, " ")
		if got != tc.want || r.Preset != tc.preset || (r.Skipped != "") != tc.skipped {
			t.Errorf("%s\n  got  %q preset %q skipped %q\n  want %q preset %q skipped=%v", tc.in, got, r.Preset, r.Skipped, tc.want, tc.preset, tc.skipped)
		}
	}
	// chain starts with npm, not a preset command, so it is not ours to touch.
	if r := s.Apply(split("npm run chain"), port, dir, noEnv); strings.Join(r.Args, " ") != "npm run chain" {
		t.Errorf("chain: %v", r.Args)
	}
	// Scripts are found from a subdirectory, as npm does.
	sub := filepath.Join(dir, "src", "app")
	os.MkdirAll(sub, 0o755)
	if r := s.Apply(split("pnpm run dev"), port, sub, noEnv); r.Preset != "vite" {
		t.Errorf("script not found from subdirectory: %+v", r)
	}
}

func TestUnsafeToAppend(t *testing.T) {
	for cmd, want := range map[string]bool{
		"vite":                   false,
		"vite --base '/a;b'":     false,
		`vite --base "/a|b"`:     false,
		"vite 2>&1":              false,
		"vite >&2":               false,
		"vite --tag v1#2":        false,
		`vite --open /foo\ #bar`: false,
		"vite && node x":         true,
		"vite dev&&node":         true,
		"tsc -w & vite":          true,
		"vite; echo done":        true,
		"vite | tee log":         true,
		"vite # comment":         true,
		"vite\nnode":             true,
		"vite \\\n# note":        true,
		"vite &> out.log":        true,
		"vite dev --port 1 || x": true,
	} {
		if got := unsafeToAppend(cmd); got != want {
			t.Errorf("unsafeToAppend(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	got := splitCommand(`vite --base '/a b/' --define "X=1 2" a\ b`)
	want := []string{"vite", "--base", "/a b/", "--define", "X=1 2", "a b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestUserPresetsOverride(t *testing.T) {
	f := filepath.Join(t.TempDir(), "presets.json")
	os.WriteFile(f, []byte(`{
		"wrappers": [{"commands": ["mise"], "subcommands": ["exec"]}],
		"presets": [
			{"name": "angular", "commands": ["ng"], "server": {"subcommands": ["serve"]},
			 "inject": [{"flags": ["--port", "{port}"], "unless": ["--port"]}]},
			{"name": "mytool", "commands": ["mytool"], "inject": [{"flags": ["--listen", ":{port}"]}]}
		]}`), 0o644)
	s, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"ng serve":         "ng serve --port 4123",
		"mise exec mytool": "mise exec mytool --listen :4123",
		"vite":             "vite --port 4123 --strictPort --host 127.0.0.1",
	} {
		if r := s.Apply(split(in), port, t.TempDir(), noEnv); strings.Join(r.Args, " ") != want {
			t.Errorf("%s: got %q, want %q", in, strings.Join(r.Args, " "), want)
		}
	}
	if s, err := Load(filepath.Join(t.TempDir(), "none.json")); err != nil || len(s.Names()) != len(Builtin().Names()) {
		t.Fatalf("missing user file: %v", err)
	}
	os.WriteFile(f, []byte(`{"presets": [{"name": "x", "commands": ["x"], "inject": [{}]}]}`), 0o644)
	if _, err := Load(f); err == nil {
		t.Fatal("invalid inject accepted")
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"vite": "vite", "/a/b/ng": "ng", "./manage.py": "manage.py", `C:\x\ng.cmd`: "ng", "npx.exe": "npx", "http.server": "http.server",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArgv(t *testing.T) {
	for in, want := range map[string][]string{
		"npm run dev":                 {"npm", "run", "dev"},
		`uvicorn "main:app" --reload`: {"uvicorn", "main:app", "--reload"},
		"npm run build && npm start":  Shell("npm run build && npm start"),
		"bundle exec puma -p $PORT":   Shell("bundle exec puma -p $PORT"),
		"python -m worker > log.txt":  Shell("python -m worker > log.txt"),
	} {
		if got := Argv(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Argv(%q) = %q, want %q", in, got, want)
		}
	}
}
