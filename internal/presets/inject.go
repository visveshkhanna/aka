package presets

import (
	"strings"
)

// inject applies pre's command-line changes to inv. It returns the new
// arguments, the words added, and why nothing was added when a preset with
// injections declined.
func (s *Set) inject(pre *Preset, inv invocation, port string) ([]string, []string, string) {
	args := inv.args
	if len(pre.Inject) == 0 {
		return args, nil, ""
	}
	pos, subIdx, ok := positionals(inv.inner, pre.ValueFlags)
	if !isServer(pre, inv.name, pos, ok) {
		return args, nil, "not a server invocation"
	}
	sub := pre.Server != nil && len(pre.Server.Subcommands) > 0 && len(pos) > 0 && contains(pre.Server.Subcommands, pos[0])

	var appended, added []string
	// Positional and flag-value inserts happen inside the argument list;
	// track them separately so indexes stay valid.
	type insertion struct {
		at    int
		words []string
	}
	var inserts []insertion
	for _, in := range pre.Inject {
		switch {
		case len(in.Flags) > 0:
			if anyOption(inv.inner, in.Unless) {
				continue
			}
			w := subst(in.Flags, port)
			appended = append(appended, w...)
		case in.Positional != "":
			// One positional is the subcommand itself, when there is one.
			existing := len(pos)
			at := inv.cmd + 1
			if sub {
				existing--
				at = inv.cmd + 1 + subIdx + 1
			}
			if existing > 0 {
				continue // runserver 8001: the user chose
			}
			inserts = append(inserts, insertion{at, []string{strings.ReplaceAll(in.Positional, "{port}", port)}})
		case in.FlagValue != "":
			k := indexOf(inv.inner, in.FlagValue)
			if k < 0 {
				continue
			}
			if k+1 < len(inv.inner) && !strings.HasPrefix(inv.inner[k+1], "-") {
				continue // php -S localhost:8000: the user chose
			}
			inserts = append(inserts, insertion{inv.cmd + 1 + k + 1, []string{strings.ReplaceAll(in.Value, "{port}", port)}})
		}
	}
	if len(appended) == 0 && len(inserts) == 0 {
		return args, nil, "the command already sets its port"
	}

	out := make([]string, 0, len(args)+len(appended)+len(inserts)+1)
	tail := appended
	if inv.separator && len(appended) > 0 {
		tail = append([]string{"--"}, appended...)
	}
	for i := 0; i <= len(args); i++ {
		for _, ins := range inserts {
			if ins.at == i {
				out = append(out, ins.words...)
				added = append(added, ins.words...)
			}
		}
		if i == inv.insert {
			out = append(out, tail...)
			added = append(added, tail...)
		}
		if i < len(args) {
			out = append(out, args[i])
		}
	}
	return out, added, ""
}

// isServer decides whether the invocation starts the preset's server.
func isServer(pre *Preset, name string, pos []string, ok bool) bool {
	sv := pre.Server
	switch {
	case sv == nil || sv.Any:
		return true
	case contains(sv.CommandIsServer, name):
		return true
	case !ok:
		return false // a flag before the subcommand we cannot classify
	case len(pos) == 0:
		return sv.Default
	case contains(sv.Subcommands, pos[0]):
		return true
	case contains(sv.NonServer, pos[0]):
		return false
	}
	return sv.PositionalRoot
}

// positionals returns the non-flag words of a command's arguments, with
// flag values skipped, and the index in args of the first one. ok is false
// when a flag of unknown arity comes before the first positional, so the
// subcommand cannot be told apart from that flag's value. A flag written as
// --name=value never consumes the next word.
func positionals(args []string, valueFlags []string) (pos []string, first int, ok bool) {
	first = -1
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			if first < 0 {
				first = i
			}
			pos = append(pos, a)
			continue
		}
		if strings.Contains(a, "=") {
			continue
		}
		if contains(valueFlags, a) {
			i++
			continue
		}
		if valueFlags == nil && len(pos) == 0 {
			return nil, -1, false
		}
	}
	return pos, first, true
}

func anyOption(args, options []string) bool {
	for _, o := range options {
		if hasOption(args, o) {
			return true
		}
	}
	return false
}

func subst(words []string, port string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strings.ReplaceAll(w, "{port}", port)
	}
	return out
}
