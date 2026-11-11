// Package config reads aka.toml, the optional project file that lists the
// apps of a multi-app project, like a Procfile:
//
//	# aka.toml
//	[apps]
//	web = "npm run dev"
//	api = "uvicorn main:app --reload"
//
//	[apps.worker]            # or a table, for more settings
//	cmd = "python -m worker"
//	dir = "services/worker"  # relative to aka.toml
//	port_hint = 9000
//
// The parser handles the subset of TOML this needs (tables, strings,
// integers, booleans, comments) and rejects anything else with a line
// number, so a typo never silently drops an app.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aka/internal/brand"
)

// FileName is the project file's name.
var FileName = brand.Name + ".toml"

// App is one app in the file.
type App struct {
	Name     string
	Command  string
	Dir      string // absolute
	PortHint int
	NoPreset bool
}

// File is a parsed project file.
type File struct {
	Path string
	Apps []App // sorted by name
}

// Find returns the nearest project file at or above dir, stopping at the
// repository root (the directory holding .git), or "" if there is none.
func Find(dir string) string {
	for d := dir; ; {
		p := filepath.Join(d, FileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return ""
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// Load parses the project file at path.
func Load(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vals, err := parse(bufio.NewScanner(f))
	if err != nil {
		return nil, fmt.Errorf("%s:%w", path, err)
	}
	out, err := build(vals, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out.Path = path
	return out, nil
}

// value is a parsed TOML value: string, int64 or bool.
type value struct {
	v    any
	line int
}

// parse reads key = value lines into "table.key" -> value.
func parse(sc *bufio.Scanner) (map[string]value, error) {
	vals := map[string]value{}
	table := ""
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(stripComment(sc.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[[") || !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("%d: unsupported table header %q", n, line)
			}
			name := strings.TrimSpace(line[1 : len(line)-1])
			if !validKey(name, true) {
				return nil, fmt.Errorf("%d: invalid table name %q", n, name)
			}
			table = name
			continue
		}
		k, raw, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%d: expected key = value", n)
		}
		k = unquoteKey(strings.TrimSpace(k))
		if !validKey(k, false) {
			return nil, fmt.Errorf("%d: invalid key %q", n, k)
		}
		v, err := parseValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%d: %s: %v", n, k, err)
		}
		full := k
		if table != "" {
			full = table + "." + k
		}
		if _, dup := vals[full]; dup {
			return nil, fmt.Errorf("%d: %s is set twice", n, full)
		}
		vals[full] = value{v, n}
	}
	return vals, sc.Err()
}

// stripComment removes a # comment that is not inside a string.
func stripComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && inD:
			i++
		case c == '"' && !inS:
			inD = !inD
		case c == '\'' && !inD:
			inS = !inS
		case c == '#' && !inS && !inD:
			return s[:i]
		}
	}
	return s
}

func unquoteKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' && k[len(k)-1] == '"' || k[0] == '\'' && k[len(k)-1] == '\'') {
		return k[1 : len(k)-1]
	}
	return k
}

func validKey(k string, dotted bool) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || dotted && r == '.') {
			return false
		}
	}
	return true
}

func parseValue(s string) (any, error) {
	switch {
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	case strings.HasPrefix(s, `"`):
		if len(s) < 2 || !strings.HasSuffix(s, `"`) {
			return nil, errors.New("unterminated string")
		}
		return strconv.Unquote(s)
	case strings.HasPrefix(s, "'"):
		if len(s) < 2 || !strings.HasSuffix(s, "'") || strings.Contains(s[1:len(s)-1], "'") {
			return nil, errors.New("unterminated string")
		}
		return s[1 : len(s)-1], nil // literal string: no escapes
	}
	if n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64); err == nil {
		return n, nil
	}
	return nil, fmt.Errorf("unsupported value %s (use a quoted string, an integer or true/false)", s)
}

// build turns parsed values into apps.
func build(vals map[string]value, base string) (*File, error) {
	apps := map[string]*App{}
	get := func(name string) *App {
		if a, ok := apps[name]; ok {
			return a
		}
		a := &App{Name: name, Dir: base}
		apps[name] = a
		return a
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := vals[k]
		rest, ok := strings.CutPrefix(k, "apps.")
		if !ok {
			return nil, fmt.Errorf("line %d: %s: unknown setting (apps go under [apps])", v.line, k)
		}
		name, field, isTable := strings.Cut(rest, ".")
		if !isTable { // web = "npm run dev"
			s, ok := v.v.(string)
			if !ok {
				return nil, fmt.Errorf("line %d: %s: the command must be a string", v.line, k)
			}
			get(name).Command = s
			continue
		}
		a := get(name)
		switch field {
		case "cmd", "command":
			s, ok := v.v.(string)
			if !ok {
				return nil, fmt.Errorf("line %d: %s must be a string", v.line, k)
			}
			a.Command = s
		case "dir":
			s, ok := v.v.(string)
			if !ok {
				return nil, fmt.Errorf("line %d: %s must be a string", v.line, k)
			}
			if !filepath.IsAbs(s) {
				s = filepath.Join(base, s)
			}
			a.Dir = filepath.Clean(s)
		case "port_hint":
			n, ok := v.v.(int64)
			if !ok || n < 1 || n > 65535 {
				return nil, fmt.Errorf("line %d: %s must be a port number", v.line, k)
			}
			a.PortHint = int(n)
		case "no_preset":
			b, ok := v.v.(bool)
			if !ok {
				return nil, fmt.Errorf("line %d: %s must be true or false", v.line, k)
			}
			a.NoPreset = b
		default:
			return nil, fmt.Errorf("line %d: %s: unknown app setting (cmd, dir, port_hint, no_preset)", v.line, k)
		}
	}
	out := &File{}
	for _, a := range apps {
		if strings.TrimSpace(a.Command) == "" {
			return nil, fmt.Errorf("app %q has no command", a.Name)
		}
		out.Apps = append(out.Apps, *a)
	}
	sort.Slice(out.Apps, func(i, j int) bool { return out.Apps[i].Name < out.Apps[j].Name })
	if len(out.Apps) == 0 {
		return nil, errors.New("no apps (add them under [apps])")
	}
	return out, nil
}

// App returns the named app.
func (f *File) App(name string) (App, bool) {
	for _, a := range f.Apps {
		if a.Name == name {
			return a, true
		}
	}
	return App{}, false
}
