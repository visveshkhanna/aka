package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/project"
	"github.com/visveshkhanna/aka/internal/runner"
)

// devCommand works out what a bare `aka` runs: the command remembered for
// this project, the only candidate found, or the one the user picks when
// there are several. It returns the directory to run it in.
func devCommand(cwd string, choose bool) (string, []string, error) {
	dir, cands := project.FindDevCommands(cwd, project.Env{LookPath: func(c string) bool {
		_, err := runner.LookPath(c, runner.AugmentPath(os.Getenv("PATH"), cwd))
		return err == nil
	}})
	choices := project.NewChoices(brand.StateDir())
	if saved, ok := choices.Get(dir); ok && !choose {
		notice("running %s (your choice for %s; `%s --choose` to change it)", strings.Join(saved, " "), dir, brand.Name)
		return dir, saved, nil
	}
	switch len(cands) {
	case 0:
		return "", nil, errors.New(noDevCommandHelp())
	case 1:
		notice("running %s (from %s)", cands[0], cands[0].Source)
		return dir, cands[0].Command, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		notice("running %s (from %s); also found: %s. Run `%s --choose` in a terminal to pick another.",
			cands[0], cands[0].Source, others(cands[1:]), brand.Name)
		return dir, cands[0].Command, nil
	}
	c, err := pick(cands)
	if err != nil {
		return "", nil, err
	}
	if err := choices.Set(dir, c.Command); err != nil {
		notice("could not remember the choice: %v", err)
	}
	return dir, c.Command, nil
}

func others(cs []project.Candidate) string {
	s := make([]string, len(cs))
	for i, c := range cs {
		s[i] = "`" + c.String() + "`"
	}
	return strings.Join(s, ", ")
}

// pick asks the user to choose a candidate on the terminal.
func pick(cands []project.Candidate) (project.Candidate, error) {
	fmt.Fprintf(os.Stderr, "[%s] Found several ways to start this project:\n", brand.Name)
	for i, c := range cands {
		fmt.Fprintf(os.Stderr, "  %d) %s    (%s)\n", i+1, c, c.Source)
	}
	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stderr, "Which one? [1-%d, default 1]: ", len(cands))
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" && err == nil {
			return cands[0], nil
		}
		if n, convErr := strconv.Atoi(line); convErr == nil && n >= 1 && n <= len(cands) {
			return cands[n-1], nil
		}
		if err != nil {
			return project.Candidate{}, errors.New("no choice made")
		}
	}
}

func noDevCommandHelp() string {
	return "no dev command found here. Give one: " + brand.Name + " <name> <command...>\n" +
		"  (looked for package.json scripts, manage.py, Procfile, Rails, Laravel, Phoenix,\n" +
		"  Spring Boot, .NET, Hugo, MkDocs, Cargo, Go and Makefile dev/run targets)"
}
