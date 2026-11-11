package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"aka/internal/brand"
)

// RunArgs is a parsed `aka [flags] [name] <command...>` invocation.
type RunArgs struct {
	Name     string // explicit name, "" to infer
	Command  []string
	Force    bool
	PortHint int
	NoPreset bool
}

// ParseRunArgs splits flags, the optional app name and the command.
//
// Flags come first and end at the first non-flag argument. Then:
//
//	aka myapp -- php -S 127.0.0.1:{port}   explicit: name before a leading "--"
//	aka --name myapp vite                  explicit via flag
//	aka myapp python manage.py runserver   "myapp" is not a command on PATH, so it is the name
//	aka uvicorn main:app                   "uvicorn" is a command, so the name is inferred
//
// isCommand reports whether a word resolves to an executable.
func ParseRunArgs(args []string, isCommand func(string) bool) (RunArgs, error) {
	var ra RunArgs
	i := 0
flags:
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" || !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		needVal := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "--force", "-f":
			ra.Force = true
		case "--no-preset":
			ra.NoPreset = true
		case "--name", "-n":
			v, err := needVal()
			if err != nil {
				return ra, err
			}
			ra.Name = v
		case "--port-hint":
			v, err := needVal()
			if err != nil {
				return ra, err
			}
			p, err := strconv.Atoi(v)
			if err != nil || p < 1 || p > 65535 {
				return ra, fmt.Errorf("--port-hint: invalid port %q", v)
			}
			ra.PortHint = p
		default:
			break flags // unknown dash-arg: treat as the start of the command
		}
	}
	rest := args[i:]

	// "--" separates the name only as the first or second word; later it
	// belongs to the command (`aka web npm run dev -- --open`).
	if dd := indexOf(rest, "--"); dd == 0 || dd == 1 {
		before, after := rest[:dd], rest[dd+1:]
		switch {
		case len(before) == 1 && ra.Name != "":
			return ra, errors.New("name given twice (--name and before --)")
		case len(before) == 1:
			ra.Name = before[0]
		}
		ra.Command = after
	} else if ra.Name == "" && len(rest) >= 2 && isValidName(rest[0]) && !looksLikeCommand(rest[0], isCommand) {
		ra.Name, ra.Command = rest[0], rest[1:]
	} else {
		ra.Command = rest
	}
	if len(ra.Command) == 0 {
		return ra, fmt.Errorf("no command given. Usage: %s <name> <command...>", brand.Name)
	}
	return ra, nil
}

func looksLikeCommand(word string, isCommand func(string) bool) bool {
	if strings.ContainsAny(word, `/\`) || strings.HasPrefix(word, ".") {
		return true // ./manage.py, bin/dev, ../x
	}
	return isCommand(word)
}

func isValidName(s string) bool {
	_, err := ValidateName(s)
	return err == nil
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

// ValidateName checks an explicit app name: DNS labels of [a-z0-9-],
// separated by dots, not starting or ending with '-'. Upper case is folded.
func ValidateName(name string) (string, error) {
	n := strings.ToLower(strings.TrimSuffix(name, "."+brand.DefaultTLD))
	if n == "" {
		return "", errors.New("empty name")
	}
	if len(n) > 200 {
		return "", fmt.Errorf("name %q is too long", name)
	}
	for _, label := range strings.Split(n, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid name %q: use letters, digits and '-', optionally dot-separated", name)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", fmt.Errorf("invalid name %q: use letters, digits and '-', optionally dot-separated", name)
			}
		}
	}
	return n, nil
}

// SanitizeName turns an arbitrary string (a directory name, a package name
// like @scope/my_app) into a valid single DNS label, or "" if nothing is left.
func SanitizeName(s string) string {
	s = strings.ToLower(s)
	if i := strings.LastIndex(s, "/"); i >= 0 { // @scope/pkg -> pkg
		s = s[i+1:]
	}
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

// InferName is the MVP name inference: the current directory's name.
// (Manifest-based inference across ecosystems is build step 4.)
func InferName(cwd string) string {
	if n := SanitizeName(filepath.Base(cwd)); n != "" {
		return n
	}
	return "app"
}

// Hostname appends the TLD to a validated name.
func Hostname(name string) string { return name + "." + brand.DefaultTLD }
