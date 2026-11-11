package presets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// packageManagers delegate `<pm> run <script>` to a package.json script.
var packageManagers = map[string]bool{"npm": true, "pnpm": true, "yarn": true, "bun": true}

// applyScript handles `<pm> run <script> [args]`: the framework lives in
// package.json, where argv inspection cannot see it. Resolve the script,
// work out the flags its command needs, and forward them through the
// package manager (`npm run dev -- --port 4123`). ok is false when args are
// not such a command or the script does not exist.
func (s *Set) applyScript(args []string, port, cwd string, getenv func(string) string) (Result, bool) {
	res := Result{Args: args}
	if len(args) < 3 || !packageManagers[normalize(args[0])] {
		return res, false
	}
	if args[1] != "run" && !(normalize(args[0]) == "npm" && args[1] == "run-script") {
		return res, false
	}
	name := args[2]
	if strings.HasPrefix(name, "-") {
		return res, false
	}
	raw, ok := packageScript(cwd, name)
	if !ok {
		return res, false
	}
	tokens := splitCommand(raw)
	for len(tokens) > 0 && assignment.MatchString(tokens[0]) {
		tokens = tokens[1:] // NODE_ENV=development vite
	}
	if len(tokens) == 0 {
		return res, true
	}

	// The user's extra args reach the script's command too, so a --port
	// there (or in the script) suppresses ours.
	var extras []string
	for _, a := range args[3:] {
		if a != "--" {
			extras = append(extras, a)
		}
	}
	probe := append(append([]string{}, tokens...), extras...)
	inv := s.unwrap(probe)
	pre := s.match(inv)
	if pre == nil {
		return res, true
	}
	res.Preset = pre.Name
	res.Env = envFor(pre, port, getenv)
	if unsafeToAppend(raw) {
		res.Skipped = "the script " + name + " is a compound shell command, so flags cannot be appended safely"
		return res, true
	}
	if inv.insert < len(tokens) || inv.separator {
		// A `--` inside the script ends the command's options, or the
		// flags would need a separator we cannot add through the script.
		res.Skipped = "the script " + name + " passes its arguments on with --"
		return res, true
	}
	out, _, why := s.inject(pre, inv, port)
	if why != "" {
		res.Skipped = why
		return res, true
	}
	// Only appending at the end can be forwarded through a script.
	if len(out) < len(probe) || !equal(out[:len(probe)], probe) {
		res.Skipped = "the preset needs to insert arguments inside the script"
		return res, true
	}
	tail := out[len(probe):]
	res.Args = append([]string{}, args...)
	if normalize(args[0]) == "npm" && indexOf(args, "--") < 0 {
		res.Args = append(res.Args, "--") // npm only forwards after --
	}
	res.Args = append(res.Args, tail...)
	res.Added = tail
	return res, true
}

// packageScript reads scripts[name] from the nearest package.json at or
// above dir, the one the package manager will use.
func packageScript(dir, name string) (string, bool) {
	for {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(b, &pkg) != nil {
				return "", false
			}
			s, ok := pkg.Scripts[name]
			return s, ok
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// splitCommand splits a shell command into words, honouring single and
// double quotes and backslash escapes. It does not interpret operators;
// unsafeToAppend decides whether the string is simple enough to use.
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	inWord, single, double, escaped := false, false, false, false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped, inWord = false, true
		case r == '\\' && !single:
			escaped, inWord = true, true
		case r == '\'' && !double:
			single, inWord = !single, true
		case r == '"' && !single:
			double, inWord = !double, true
		case (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !single && !double:
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

// unsafeToAppend reports whether appending arguments to the raw script
// would change what the shell runs. Translated from portless's
// isUnsafeToAppendArgs: a control operator or newline starts a new command,
// so appended flags reach the wrong one, and a word-initial # comments them
// out. Quoted metacharacters, fd duplication (2>&1) and a # inside a word
// are fine.
func unsafeToAppend(cmd string) bool {
	single, double, escaped := false, false, false
	atWordStart := true
	rs := []rune(cmd)
	for i, ch := range rs {
		if escaped {
			escaped = false
			if ch == '\n' || ch == '\r' {
				continue // line continuation keeps word position
			}
			atWordStart = false
			continue
		}
		if ch == '\\' && !single {
			escaped = true
			continue
		}
		if ch == '\'' && !double {
			single = !single
			atWordStart = false
			continue
		}
		if ch == '"' && !single {
			double = !double
			atWordStart = false
			continue
		}
		if single || double {
			continue
		}
		switch ch {
		case ';', '\n', '\r', '|':
			return true
		case '#':
			if atWordStart {
				return true
			}
		case '&':
			// Only POSIX fd duplication (2>&1, >&2, 2>&-) is a redirection.
			if i > 0 && rs[i-1] == '>' && i+1 < len(rs) && (rs[i+1] >= '0' && rs[i+1] <= '9' || rs[i+1] == '-') {
				break
			}
			return true
		}
		atWordStart = ch == ' ' || ch == '\t'
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Argv turns a command line written as one string (aka.toml, a Procfile)
// into arguments. A line the shell would treat as more than one command,
// or that uses a comment, runs through sh -c; a simple one is split into
// words with quotes honoured, so presets can see it.
func Argv(line string) []string {
	if unsafeToAppend(line) || strings.ContainsAny(line, "$`<>*?(){}") {
		return []string{"sh", "-c", line}
	}
	return splitCommand(line)
}
