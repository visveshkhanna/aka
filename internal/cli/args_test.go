package cli

import (
	"reflect"
	"strings"
	"testing"
)

// onPath fakes PATH lookup.
func onPath(cmds ...string) func(string) bool {
	set := map[string]bool{}
	for _, c := range cmds {
		set[c] = true
	}
	return func(w string) bool { return set[w] }
}

func TestParseRunArgs(t *testing.T) {
	isCmd := onPath("python", "uvicorn", "ng", "php", "npm", "npx", "vite", "test")
	cases := []struct {
		name string
		args []string
		want RunArgs
		err  string
	}{
		{"name then command", []string{"myapp", "python", "manage.py", "runserver"},
			RunArgs{Name: "myapp", Command: []string{"python", "manage.py", "runserver"}}, ""},
		{"command only, name inferred", []string{"uvicorn", "main:app", "--reload"},
			RunArgs{Command: []string{"uvicorn", "main:app", "--reload"}}, ""},
		{"single command word", []string{"vite"}, RunArgs{Command: []string{"vite"}}, ""},
		{"ng serve", []string{"ng", "serve"}, RunArgs{Command: []string{"ng", "serve"}}, ""},
		{"explicit -- with placeholder", []string{"api", "--", "php", "-S", "127.0.0.1:{port}"},
			RunArgs{Name: "api", Command: []string{"php", "-S", "127.0.0.1:{port}"}}, ""},
		{"-- without name", []string{"--", "python", "app.py"}, RunArgs{Command: []string{"python", "app.py"}}, ""},
		{"--name flag", []string{"--name", "test", "test", "-f", "x"},
			RunArgs{Name: "test", Command: []string{"test", "-f", "x"}}, ""},
		{"--name=value", []string{"--name=web", "npm", "run", "dev"},
			RunArgs{Name: "web", Command: []string{"npm", "run", "dev"}}, ""},
		{"flags before name", []string{"--force", "--port-hint", "8000", "web", "python", "app.py"},
			RunArgs{Name: "web", Force: true, PortHint: 8000, Command: []string{"python", "app.py"}}, ""},
		{"short force", []string{"-f", "web", "vite"}, RunArgs{Name: "web", Force: true, Command: []string{"vite"}}, ""},
		{"no preset", []string{"--no-preset", "web", "vite"}, RunArgs{Name: "web", NoPreset: true, Command: []string{"vite"}}, ""},
		{"flags after name belong to the command", []string{"web", "vite", "--force", "--port", "1"},
			RunArgs{Name: "web", Command: []string{"vite", "--force", "--port", "1"}}, ""},
		{"-- inside command after name", []string{"web", "npm", "run", "dev", "--", "--open"},
			RunArgs{Name: "web", Command: []string{"npm", "run", "dev", "--", "--open"}}, ""},
		{"-- inside command without name", []string{"npm", "run", "dev", "--", "--open"},
			RunArgs{Command: []string{"npm", "run", "dev", "--", "--open"}}, ""},
		{"path-like first word is a command", []string{"./manage.py", "runserver"},
			RunArgs{Command: []string{"./manage.py", "runserver"}}, ""},
		{"bin/dev is a command", []string{"bin/dev"}, RunArgs{Command: []string{"bin/dev"}}, ""},
		{"unknown dash arg starts the command", []string{"-m", "http.server"},
			RunArgs{Command: []string{"-m", "http.server"}}, ""},
		{"name with no command", []string{"myapp", "--"}, RunArgs{}, "no command"},
		{"nothing", []string{}, RunArgs{}, "no command"},
		{"--name without value", []string{"--name"}, RunArgs{}, "needs a value"},
		{"bad port hint", []string{"--port-hint", "99999", "vite"}, RunArgs{}, "invalid port"},
		{"name twice", []string{"--name", "a", "b", "--", "vite"}, RunArgs{}, "name given twice"},
		{"invalid name word is a command", []string{"my_app", "x"}, RunArgs{Command: []string{"my_app", "x"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseRunArgs(c.args, isCmd)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want containing %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Command == nil {
				got.Command = []string{}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	ok := map[string]string{
		"myapp":           "myapp",
		"MyApp":           "myapp",
		"api.myapp":       "api.myapp",
		"my-app2":         "my-app2",
		"myapp.localhost": "myapp",
	}
	for in, want := range ok {
		if got, err := ValidateName(in); err != nil || got != want {
			t.Errorf("ValidateName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-app", "app-", "my_app", "a..b", ".a", "a b", "ünï", strings.Repeat("a", 64), "x." + strings.Repeat("a", 200)} {
		if _, err := ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) accepted", bad)
		}
	}
}

func TestSanitizeAndInferName(t *testing.T) {
	cases := map[string]string{
		"My_Project":            "my-project",
		"@scope/web-app":        "web-app",
		"  spaced  out  ":       "spaced-out",
		"___":                   "",
		"app.v2":                "app-v2",
		strings.Repeat("a", 70): strings.Repeat("a", 63),
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := InferName("/home/me/code/My Site"); got != "my-site" {
		t.Errorf("InferName = %q", got)
	}
	if got := InferName("/___"); got != "app" {
		t.Errorf("InferName fallback = %q", got)
	}
	if Hostname("web") != "web.localhost" {
		t.Error(Hostname("web"))
	}
}
