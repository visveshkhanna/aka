package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"github.com/visveshkhanna/aka/internal/config"
	"github.com/visveshkhanna/aka/internal/daemon"
	"github.com/visveshkhanna/aka/internal/presets"
)

// cmdUp runs the apps of aka.toml together: `aka up` for all of them,
// `aka up web api` for some. Each app runs as its own aka (so it gets its
// own route, presets and detection), with its output prefixed by its name.
func cmdUp(args []string) int {
	cwd, _ := os.Getwd()
	path := config.Find(cwd)
	if path == "" {
		return fail(fmt.Errorf("no %s found here or above (up to the repository root)", config.FileName))
	}
	f, err := config.Load(path)
	if err != nil {
		return fail(err)
	}
	return up(f, args)
}

func up(f *config.File, names []string) int {
	apps := f.Apps
	if len(names) > 0 {
		apps = nil
		for _, n := range names {
			a, ok := f.App(n)
			if !ok {
				return fail(fmt.Errorf("%s has no app %q (it has: %s)", f.Path, n, appNames(f)))
			}
			apps = append(apps, a)
		}
	}
	for _, a := range apps {
		if _, err := ValidateName(a.Name); err != nil {
			return fail(fmt.Errorf("%s: app %q: %w", f.Path, a.Name, err))
		}
	}

	// Prepare once here, so the apps do not race to prompt or to start it.
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	if so.TLS {
		ensureTrust(so.StateDir)
	}
	if _, err := daemon.Ensure(so); err != nil {
		return fail(err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	color := term.IsTerminal(int(os.Stdout.Fd()))
	width := 0
	for _, a := range apps {
		width = max(width, len(a.Name))
	}
	notice("starting %s from %s", appList(apps), f.Path)

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, upSignals...)
	defer signal.Stop(sigs)

	var mu sync.Mutex
	var procs []*os.Process
	var wg sync.WaitGroup
	codes := make([]int, len(apps))
	for i, a := range apps {
		args := []string{"--name", a.Name}
		if a.PortHint > 0 {
			args = append(args, "--port-hint", strconv.Itoa(a.PortHint))
		}
		if a.NoPreset {
			args = append(args, "--no-preset")
		}
		args = append(append(args, "--"), presets.Argv(a.Command)...)
		cmd := exec.Command(exe, args...)
		cmd.Dir = a.Dir
		upIsolate(cmd)
		prefix := fmt.Sprintf("%-*s | ", width, a.Name)
		if color {
			prefix = "\x1b[" + palette[i%len(palette)] + "m" + prefix + "\x1b[0m"
		}
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			notice("%s: %v", a.Name, err)
			codes[i] = 1
			continue
		}
		upTrack(cmd.Process)
		mu.Lock()
		procs = append(procs, cmd.Process)
		mu.Unlock()
		var copied sync.WaitGroup
		copied.Add(2)
		go func() { defer copied.Done(); prefixLines(os.Stdout, stdout, prefix) }()
		go func() { defer copied.Done(); prefixLines(os.Stderr, stderr, prefix) }()
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			copied.Wait() // read all output before Wait closes the pipes
			err := cmd.Wait()
			codes[i] = exitStatus(err)
			if codes[i] != 0 {
				notice("%s exited with code %d", name, codes[i])
			}
		}(i, a.Name)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-done:
			for _, c := range codes {
				if c != 0 {
					return c
				}
			}
			return 0
		case sig := <-sigs:
			mu.Lock()
			for _, p := range procs {
				upForward(p, sig)
			}
			mu.Unlock()
		}
	}
}

var palette = []string{"36", "35", "33", "32", "34", "31", "96", "95"}

// prefixLines copies r to w, starting every line with prefix. Lines are
// written whole, so output from different apps does not interleave
// mid-line.
func prefixLines(w io.Writer, r io.Reader, prefix string) {
	br := bufio.NewReaderSize(r, 64<<10)
	var mu = &outMu
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			mu.Lock()
			io.WriteString(w, prefix+line)
			mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

var outMu sync.Mutex

func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if c := ee.ExitCode(); c >= 0 {
			return c
		}
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
	}
	return 1
}

func appNames(f *config.File) string {
	n := make([]string, len(f.Apps))
	for i, a := range f.Apps {
		n[i] = a.Name
	}
	return strings.Join(n, ", ")
}

func appList(apps []config.App) string {
	n := make([]string, len(apps))
	for i, a := range apps {
		n[i] = a.Name
	}
	return strings.Join(n, ", ")
}
