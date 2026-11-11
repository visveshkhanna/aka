// Package cli is the command-line entry point.
package cli

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"aka/internal/brand"
	"aka/internal/daemon"
	"aka/internal/presets"
	"aka/internal/project"
	"aka/internal/proxy"
	"aka/internal/runner"
)

// Version is set at build time with -ldflags "-X aka/internal/cli.Version=...".
var Version = "dev"

const daemonCmd = "__daemon"

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	if len(args) == 0 {
		return cmdRun(nil) // bare aka: run the project's dev command
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	case "-v", "--version", "version":
		fmt.Println(brand.Name, Version)
		return 0
	case "list", "ls":
		return cmdList()
	case "proxy":
		return cmdProxy(args[1:])
	case "alias":
		return cmdAlias(args[1:])
	case "trust":
		return cmdTrust()
	case "untrust":
		return cmdUntrust()
	case daemonCmd:
		return cmdDaemon(args[1:])
	case "run":
		return cmdRun(args[1:])
	}
	return cmdRun(args)
}

func usage(w io.Writer) {
	n := brand.Name
	fmt.Fprintf(w, `%[1]s gives a local dev server a stable URL: https://<name>.localhost

Usage:
  %[1]s                            run this project's dev command (package.json, manage.py, ...)
  %[1]s <name> <command...>        run command, route <name>.localhost to it
  %[1]s <command...>               same, name inferred from the directory
  %[1]s <name> -- <command...>     explicit name (use {port} in the command for the port)
  %[1]s list                       show active routes
  %[1]s alias <name> <port>        route <name>.localhost to a port aka did not start
  %[1]s alias [rm <name>]          list or remove aliases
  %[1]s proxy start|stop|status    manage the proxy daemon
  sudo %[1]s proxy start           also serve port 443 (root is dropped after binding)
  %[1]s trust | untrust            add or remove the local CA in the system trust store

Flags (before the name):
  --name, -n <name>   app name (when it would be mistaken for a command)
  --force, -f         take over a name another %[1]s is using
  --port-hint <port>  route to this port instead of detecting one
  --no-preset         do not add port flags or env for known frameworks
  --choose            pick again which dev command a bare %[1]s runs

The command gets PORT=<free port>, HOST=127.0.0.1 and %[3]s=<public url>.
If it ignores PORT, %[1]s routes to whatever port it actually listens on.

Environment:
  %[4]s   proxy port for HTTP and HTTPS (default %[2]d)
  %[8]s   dedicated HTTPS port to try (default %[9]d, 0 for none)
  %[10]s        0 for plain HTTP only
  %[5]s    state directory (default ~/%[6]s)
  %[7]s       disable pseudo-terminal output capture
`, n, brand.DefaultProxyPort, brand.Env("URL"), brand.Env("PROXY_PORT"), brand.Env("STATE_DIR"), brand.StateDirName, brand.Env("NO_PTY"),
		brand.Env("HTTPS_PORT"), brand.DefaultHTTPSPort, brand.Env("HTTPS"))
}

func proxyPort() (int, error) {
	v := os.Getenv(brand.Env("PROXY_PORT"))
	if v == "" {
		return brand.DefaultProxyPort, nil
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("%s: invalid port %q", brand.Env("PROXY_PORT"), v)
	}
	return p, nil
}

func startOptions() (daemon.StartOptions, error) {
	port, err := proxyPort()
	if err != nil {
		return daemon.StartOptions{}, err
	}
	hport, err := httpsPort()
	if err != nil {
		return daemon.StartOptions{}, err
	}
	args := []string{daemonCmd, "--port", strconv.Itoa(port)}
	tls := httpsEnabled()
	if tls {
		args = append(args, "--https-port", strconv.Itoa(hport))
	} else {
		args = append(args, "--no-tls")
	}
	return daemon.StartOptions{
		StateDir:  brand.StateDir(),
		ProxyPort: port,
		TLS:       tls,
		Args:      args,
		Warn:      func(s string) { notice("%s", s) },
	}, nil
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "%s: %v\n", brand.Name, err)
	return 1
}

func cmdRun(args []string) int {
	cwd, _ := os.Getwd()
	path := runner.AugmentPath(os.Getenv("PATH"), cwd)
	ra, err := ParseRunArgs(args, func(w string) bool {
		_, err := runner.LookPath(w, path)
		return err == nil
	})
	if err != nil {
		return fail(err)
	}
	dir := cwd
	command := ra.Command
	if len(command) == 0 {
		if dir, command, err = devCommand(cwd, ra.Choose); err != nil {
			return fail(err)
		}
	}
	var name string
	if ra.Name != "" {
		if name, err = ValidateName(ra.Name); err != nil {
			return fail(err)
		}
	} else {
		name = project.InferName(dir).Name
	}
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	if so.TLS {
		ensureTrust(so.StateDir)
	}
	var ps *presets.Set
	if !ra.NoPreset {
		if ps, err = presets.Load(filepath.Join(so.StateDir, "presets.json")); err != nil {
			notice("ignoring your presets file: %v", err)
		}
	}
	code, err := runner.Run(runner.Options{
		Hostname: Hostname(name),
		Args:     command,
		Dir:      dir,
		Force:    ra.Force,
		PortHint: ra.PortHint,
		Daemon:   so,
		Presets:  ps,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", brand.Name, err)
	}
	return code
}

func cmdList() int {
	resp, err := daemon.Do(brand.StateDir(), daemon.Request{Op: daemon.OpList})
	if err != nil {
		fmt.Println("No routes (proxy is not running).")
		return 0
	}
	ping, _ := daemon.Ping(brand.StateDir())
	printRoutes(os.Stdout, resp.Routes, ping)
	return 0
}

func printRoutes(w io.Writer, routes []proxy.Route, ping daemon.Response) {
	if len(routes) == 0 {
		fmt.Fprintln(w, "No routes.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "URL\tTARGET\tPID\tUP\tCOMMAND")
	for _, r := range routes {
		target := "starting"
		if r.Port > 0 {
			h := r.DialHost
			if h == "" {
				h = "localhost"
			}
			target = net.JoinHostPort(h, strconv.Itoa(r.Port))
		}
		up := ""
		if !r.Since.IsZero() {
			up = time.Since(r.Since).Round(time.Second).String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", ping.URL(r.Hostname), target, r.PID, up, r.Command)
	}
	tw.Flush()
}

func cmdProxy(args []string) int {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	dir := brand.StateDir()
	switch sub {
	case "start":
		if os.Geteuid() == 0 {
			return startPrivileged()
		}
		return startProxy(nil)
	case "stop":
		if _, err := daemon.Ping(dir); err != nil {
			fmt.Println("Proxy is not running.")
			return 0
		}
		if err := daemon.Stop(dir, 5*time.Second); err != nil {
			return fail(err)
		}
		fmt.Println("Proxy stopped.")
	case "status":
		r, err := daemon.Ping(dir)
		if err != nil {
			fmt.Println("Proxy is not running.")
			return 1
		}
		list, _ := daemon.Do(dir, daemon.Request{Op: daemon.OpList})
		fmt.Println(describeProxy(r))
		if note := daemon.HTTPSNote(r); note != "" {
			fmt.Println(note)
		}
		fmt.Printf("%d route(s). Log: %s\n", len(list.Routes), daemon.LogPath(dir))
		if r.BuildID != daemon.BuildID() {
			fmt.Println("Note: the daemon is running a different build of " + brand.Name + "; restart it with `" + brand.Name + " proxy stop`.")
		}
	default:
		return fail(fmt.Errorf("unknown proxy command %q (want start, stop or status)", sub))
	}
	return 0
}

// cmdDaemon runs the daemon in the foreground. The CLI starts it detached.
func cmdDaemon(args []string) int {
	// The daemon reads its whole configuration from args; see startOptions.
	port, err := proxyPort()
	if err != nil {
		return fail(err)
	}
	cfg := daemon.Config{
		StateDir:  brand.StateDir(),
		ProxyPort: port,
		TLS:       true,
		Log:       log.New(os.Stderr, "", log.LstdFlags),
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := ""
		if i+1 < len(args) {
			val = args[i+1]
		}
		switch a {
		case "--port", "--https-port":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fail(fmt.Errorf("%s: %w", a, err))
			}
			if a == "--port" {
				cfg.ProxyPort = n
			} else {
				cfg.HTTPSPort = n
			}
			i++
		case "--no-tls":
			cfg.TLS = false
		case "--https-fds":
			lns, err := inheritedListeners(val)
			if err != nil {
				return fail(err)
			}
			cfg.HTTPSListeners = lns
			i++
		}
	}
	port = cfg.ProxyPort
	s, err := daemon.Start(cfg)
	if errors.Is(err, daemon.ErrAlreadyRunning) {
		fmt.Fprintln(os.Stderr, "daemon already running")
		return 0
	}
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") && port < 1024 {
			err = fmt.Errorf("%w (ports below 1024 need privileges)", err)
		}
		return fail(err)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	signal.Ignore(syscall.SIGHUP)
	select {
	case <-sigs:
		s.Close()
	case <-s.Done():
	}
	return 0
}

// inheritedListeners turns "3,4" into listeners on those file descriptors,
// passed by `sudo aka proxy start` after binding them as root.
func inheritedListeners(fds string) ([]net.Listener, error) {
	var lns []net.Listener
	for _, f := range strings.Split(fds, ",") {
		fd, err := strconv.Atoi(f)
		if err != nil || fd < 3 {
			return nil, fmt.Errorf("--https-fds: invalid fd %q", f)
		}
		file := os.NewFile(uintptr(fd), "https-listener")
		ln, err := net.FileListener(file)
		file.Close() // FileListener duplicated it
		if err != nil {
			return nil, fmt.Errorf("--https-fds: fd %d: %w", fd, err)
		}
		lns = append(lns, ln)
	}
	return lns, nil
}
