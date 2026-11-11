// Package presets makes dev servers that ignore PORT listen where aka wants
// them. A data table (presets.json, overridable per user) maps a command,
// found by looking past wrappers such as npx, poetry run or python -m, to the
// flags, positional argument or environment variables that set its port.
//
// Injection is conservative: only a server subcommand gets flags (never
// `vite build` or `ng test`), a flag the user already gave is never added
// twice, and a package script that the shell would re-parse unsafely is left
// alone. Anything not covered still works through listener detection.
package presets

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

//go:embed presets.json
var builtinJSON []byte

// File is the presets file format.
type File struct {
	Wrappers []Wrapper `json:"wrappers"`
	Presets  []Preset  `json:"presets"`
}

// Wrapper is a command that runs another command: a package runner (npx,
// poetry run), an interpreter (python -m mod, python script.py) or an env
// setter (cross-env A=b cmd).
type Wrapper struct {
	Commands []string `json:"commands"`
	// Subcommands that run another command (`pnpm exec`). Empty means the
	// command itself runs the next word (`npx vite`).
	Subcommands []string `json:"subcommands,omitempty"`
	// ValueFlags consume the next word.
	ValueFlags []string `json:"valueFlags,omitempty"`
	// SeparatorRequired means flags for the inner command must follow a
	// `--` (npm exec).
	SeparatorRequired bool `json:"separatorRequired,omitempty"`
	// SkipAssignments skips KEY=value words (cross-env, env).
	SkipAssignments bool `json:"skipAssignments,omitempty"`
	// Interpreter: the first positional is a script, whose base name
	// becomes the command (python manage.py -> manage.py).
	Interpreter bool `json:"interpreter,omitempty"`
	// ModuleFlag names a module to run instead (python -m uvicorn).
	ModuleFlag string `json:"moduleFlag,omitempty"`
	// StopFlags end unwrapping: the interpreter itself is the command
	// (php -S, python -c).
	StopFlags []string `json:"stopFlags,omitempty"`
}

// Preset says how to set one command's port.
type Preset struct {
	Name string `json:"name"`
	// Commands this preset applies to, after unwrapping.
	Commands []string `json:"commands,omitempty"`
	// Via applies the preset to anything run through one of these
	// wrappers (any python script, for Gradio) when no preset matches the
	// command itself. Use it only for env presets: they are harmless when
	// the guess is wrong.
	Via []string `json:"via,omitempty"`
	// Server decides which invocations are servers. Nil means all.
	Server *Server `json:"server,omitempty"`
	// ValueFlags consume the next word, so a flag value is not mistaken
	// for the subcommand (`vite --mode dev build`).
	ValueFlags []string `json:"valueFlags,omitempty"`
	Inject     []Inject `json:"inject,omitempty"`
	// Env is set for the app unless the user already set it.
	Env map[string]string `json:"env,omitempty"`
}

// Server classifies an invocation as starting a server or not.
type Server struct {
	// Any: every invocation is a server (uvicorn main:app).
	Any bool `json:"any,omitempty"`
	// Subcommands that start a server.
	Subcommands []string `json:"subcommands,omitempty"`
	// NonServer subcommands; unknown ones are declined unless
	// PositionalRoot is set.
	NonServer []string `json:"nonServer,omitempty"`
	// Default: a bare invocation (no subcommand) is a server.
	Default bool `json:"default,omitempty"`
	// PositionalRoot: an unknown first positional is a root directory and
	// the invocation is a server (vite ./app).
	PositionalRoot bool `json:"positionalRoot,omitempty"`
	// CommandIsServer lists command names that are servers whatever
	// follows (jupyter-lab).
	CommandIsServer []string `json:"commandIsServer,omitempty"`
}

// Inject is one change to the command line. Exactly one of Flags,
// Positional or FlagValue is set.
type Inject struct {
	// Flags are appended, unless any flag in Unless is already given.
	Flags  []string `json:"flags,omitempty"`
	Unless []string `json:"unless,omitempty"`
	// Positional is inserted after the subcommand, unless a positional
	// argument is already there (runserver 8001).
	Positional string `json:"positional,omitempty"`
	// FlagValue gives Value to a flag the user wrote without one
	// (`php -S` -> `php -S 127.0.0.1:4123`).
	FlagValue string `json:"flagValue,omitempty"`
	Value     string `json:"value,omitempty"`
}

// Set is a loaded table.
type Set struct {
	wrappers map[string]*Wrapper
	presets  []Preset
}

// Builtin returns the presets shipped with aka.
func Builtin() *Set {
	s, err := parse(builtinJSON)
	if err != nil {
		panic("presets.json: " + err.Error()) // caught by tests
	}
	return s
}

// Load returns the built-in presets with the user's file, if it exists,
// layered on top: a user preset replaces a built-in one of the same name,
// and user wrappers replace built-in ones for the same command.
func Load(userFile string) (*Set, error) {
	s := Builtin()
	b, err := os.ReadFile(userFile)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return s, fmt.Errorf("%s: %w", userFile, err)
	}
	if err := validate(f); err != nil {
		return s, fmt.Errorf("%s: %w", userFile, err)
	}
	for i := range f.Wrappers {
		for _, c := range f.Wrappers[i].Commands {
			s.wrappers[c] = &f.Wrappers[i]
		}
	}
	user := map[string]bool{}
	for _, p := range f.Presets {
		user[p.Name] = true
	}
	merged := append([]Preset{}, f.Presets...)
	for _, p := range s.presets {
		if !user[p.Name] {
			merged = append(merged, p)
		}
	}
	s.presets = merged
	return s, nil
}

func parse(b []byte) (*Set, error) {
	var f File
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	if err := validate(f); err != nil {
		return nil, err
	}
	s := &Set{wrappers: map[string]*Wrapper{}, presets: f.Presets}
	for i := range f.Wrappers {
		for _, c := range f.Wrappers[i].Commands {
			s.wrappers[c] = &f.Wrappers[i]
		}
	}
	return s, nil
}

func validate(f File) error {
	for _, p := range f.Presets {
		if p.Name == "" || len(p.Commands)+len(p.Via) == 0 {
			return fmt.Errorf("preset %q needs a name and commands or via", p.Name)
		}
		if len(p.Via) > 0 && len(p.Inject) > 0 {
			return fmt.Errorf("preset %q: via presets may only set env", p.Name)
		}
		for _, in := range p.Inject {
			n := 0
			if len(in.Flags) > 0 {
				n++
			}
			if in.Positional != "" {
				n++
			}
			if in.FlagValue != "" {
				n++
			}
			if n != 1 {
				return fmt.Errorf("preset %q: each inject needs exactly one of flags, positional or flagValue", p.Name)
			}
		}
	}
	return nil
}

// Names lists the presets in match order.
func (s *Set) Names() []string {
	out := make([]string, len(s.presets))
	for i, p := range s.presets {
		out[i] = p.Name
	}
	return out
}

// Result is what Apply decided.
type Result struct {
	// Preset is the name of the preset applied, "" if none.
	Preset string
	// Args is the command to run.
	Args []string
	// Added are the words inserted into Args, for display.
	Added []string
	// Env are variables to set for the app (only those the user has not
	// set).
	Env []string
	// Skipped explains why a matching preset injected nothing ("" if it
	// did, or none matched).
	Skipped string
}

// Apply works out the command and environment for args with the app port.
// cwd is where the command runs (for package.json scripts); getenv reports
// the user's environment, so their own settings win.
func (s *Set) Apply(args []string, port int, cwd string, getenv func(string) string) Result {
	res := Result{Args: args}
	if len(args) == 0 {
		return res
	}
	p := strconv.Itoa(port)

	if r, ok := s.applyScript(args, p, cwd, getenv); ok {
		return r
	}

	inv := s.unwrap(args)
	pre := s.match(inv)
	if pre == nil {
		return res
	}
	res.Preset = pre.Name
	res.Env = envFor(pre, p, getenv)
	out, added, why := s.inject(pre, inv, p)
	res.Args, res.Added, res.Skipped = out, added, why
	return res
}

func envFor(pre *Preset, port string, getenv func(string) string) []string {
	var env []string
	for _, k := range sortedKeys(pre.Env) {
		if getenv(k) != "" {
			continue // the user's setting wins
		}
		env = append(env, k+"="+strings.ReplaceAll(pre.Env[k], "{port}", port))
	}
	return env
}

// match finds the preset for the unwrapped command.
func (s *Set) match(inv invocation) *Preset {
	for i := range s.presets {
		if contains(s.presets[i].Commands, inv.name) {
			return &s.presets[i]
		}
	}
	for i := range s.presets {
		for _, w := range inv.chain {
			if contains(s.presets[i].Via, w) {
				return &s.presets[i]
			}
		}
	}
	return nil
}

// normalize turns a command word into the name presets match on: the base
// name, without Windows executable extensions.
func normalize(word string) string {
	name := filepath.Base(strings.ReplaceAll(word, `\`, "/"))
	if runtime.GOOS == "windows" || strings.ContainsAny(name, ".") {
		lower := strings.ToLower(name)
		for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
			if strings.HasSuffix(lower, ext) {
				return name[:len(name)-len(ext)]
			}
		}
	}
	return name
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// hasOption reports whether args contain option, alone or as option=value.
func hasOption(args []string, option string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == option || strings.HasPrefix(a, option+"=") {
			return true
		}
	}
	return false
}
