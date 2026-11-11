package presets

import (
	"regexp"
	"strings"
)

// invocation is a command line with its wrappers looked past.
type invocation struct {
	args  []string
	chain []string // wrapper names passed through, outermost first
	// cmd is the index of the command word; name is what presets match.
	cmd  int
	name string
	// inner is the command's own arguments, as it will see them.
	inner []string
	// insert is where appended flags go: before a `--` that ends the
	// command's options, else at the end.
	insert int
	// separator: a `--` must precede appended flags (npm exec).
	separator bool
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// unwrap looks past wrappers to the command that will actually run.
func (s *Set) unwrap(args []string) invocation {
	inv := invocation{args: args}
	needSep, sawSep := false, false
	i := 0
	for i < len(args) {
		name := normalize(args[i])
		w := s.wrappers[name]
		if w == nil {
			break
		}
		inv.chain = append(inv.chain, name)
		j := i + 1
		switch {
		case w.SkipAssignments:
			for j < len(args) && (assignment.MatchString(args[j]) || strings.HasPrefix(args[j], "-")) {
				j++
			}
		case w.Interpreter:
			next, name, done := interpreterTarget(w, args, j)
			if done {
				inv.cmd, inv.name = next, name
				return inv.finish(needSep, sawSep)
			}
			j = next
		default:
			j, sawSep = skipOptions(w, args, j, sawSep)
			if len(w.Subcommands) > 0 {
				if j >= len(args) || !contains(w.Subcommands, args[j]) {
					// `pnpm install`: the wrapper is the command.
					inv.cmd, inv.name = i, name
					return inv.finish(needSep, sawSep)
				}
				j, sawSep = skipOptions(w, args, j+1, sawSep)
			}
			if w.SeparatorRequired {
				needSep = true
			}
		}
		if j >= len(args) {
			inv.cmd, inv.name = i, name
			return inv.finish(false, false)
		}
		i = j
	}
	inv.cmd = i
	if i < len(args) {
		inv.name = normalize(args[i])
	}
	return inv.finish(needSep, sawSep)
}

// skipOptions skips a wrapper's own flags from j, noting a `--`.
func skipOptions(w *Wrapper, args []string, j int, sawSep bool) (int, bool) {
	for j < len(args) && strings.HasPrefix(args[j], "-") {
		if args[j] == "--" {
			return j + 1, true
		}
		if !strings.Contains(args[j], "=") && contains(w.ValueFlags, args[j]) {
			j++
		}
		j++
	}
	return j, sawSep
}

// interpreterTarget finds what an interpreter runs: a module (-m mod), a
// script, or itself (a stop flag, or nothing to run). done reports that the
// command was found at index idx with the given name.
func interpreterTarget(w *Wrapper, args []string, j int) (idx int, name string, done bool) {
	self := j - 1
	for j < len(args) {
		a := args[j]
		switch {
		case w.ModuleFlag != "" && a == w.ModuleFlag:
			if j+1 < len(args) {
				return j + 1, args[j+1], true
			}
			return self, normalize(args[self]), true
		case w.ModuleFlag != "" && strings.HasPrefix(a, w.ModuleFlag) && len(a) > len(w.ModuleFlag):
			return j, a[len(w.ModuleFlag):], true // -mhttp.server
		case contains(w.StopFlags, a):
			return self, normalize(args[self]), true
		case a == "-" || !strings.HasPrefix(a, "-"):
			if a == "-" {
				return self, normalize(args[self]), true // script on stdin
			}
			// A script: it runs with the interpreter, and may itself be a
			// wrapper-free command such as manage.py or artisan.
			return j, normalize(a), true
		case !strings.Contains(a, "=") && contains(w.ValueFlags, a):
			j += 2
		default:
			j++
		}
	}
	return self, normalize(args[self]), true
}

// finish fills in the command's own arguments and the insertion point.
func (inv invocation) finish(needSep, sawSep bool) invocation {
	args := inv.args
	if inv.cmd >= len(args) {
		inv.insert = len(args)
		return inv
	}
	rest := args[inv.cmd+1:]
	dd := indexOf(rest, "--")
	switch {
	case needSep && !sawSep && dd < 0:
		// npm exec vite: flags for vite must come after a `--`.
		inv.insert, inv.separator = len(args), true
		inv.inner = rest
	case needSep && !sawSep:
		// npm exec vite -- --open: what follows `--` is vite's.
		inv.insert = len(args)
		inv.inner = append(append([]string{}, rest[:dd]...), rest[dd+1:]...)
	case dd >= 0:
		inv.insert = inv.cmd + 1 + dd
		inv.inner = rest[:dd]
	default:
		inv.insert = len(args)
		inv.inner = rest
	}
	return inv
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
