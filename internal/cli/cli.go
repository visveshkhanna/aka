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
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"aka/internal/brand"
	"aka/internal/daemon"
	"aka/internal/proxy"
	"aka/internal/runner"
)

// Version is set at build time with -ldflags "-X aka/internal/cli.Version=...".
var Version = "dev"

const daemonCmd = "__daemon"

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
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
	case daemonCmd:
		return cmdDaemon(args[1:])
	case "run":
		return cmdRun(args[1:])
	}
	return cmdRun(args)
}

func usage(w io.Writer) {
	n := brand.Name
	fmt.Fprintf(w, `%[1]s gives a local dev server a stable URL: http://<name>.localhost:%[2]d

Usage:
  %[1]s <name> <command...>        run command, route <name>.localhost to it
  %[1]s <command...>               same, name inferred from the directory
  %[1]s <name> -- <command...>     explicit name (use {port} in the command for the port)
  %[1]s list                       show active routes
  %[1]s proxy start|stop|status    manage the proxy daemon

Flags (before the name):
  --name, -n <name>   app name (when it would be mistaken for a command)
  --force, -f         take over a name another %[1]s is using
  --port-hint <port>  route to this port instead of detecting one

The command gets PORT=<free port>, HOST=127.0.0.1 and %[3]s=<public url>.
If it ignores PORT, %[1]s routes to whatever port it actually listens on.

Environment:
  %[4]s   proxy port (default %[2]d)
  %[5]s    state directory (default ~/%[6]s)
  %[7]s       disable pseudo-terminal output capture
`, n, brand.DefaultProxyPort, brand.Env("URL"), brand.Env("PROXY_PORT"), brand.Env("STATE_DIR"), brand.StateDirName, brand.Env("NO_PTY"))
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
	return daemon.StartOptions{
		StateDir:  brand.StateDir(),
		ProxyPort: port,
		Args:      []string{daemonCmd, "--port", strconv.Itoa(port)},
		Warn:      func(s string) { fmt.Fprintf(os.Stderr, "[%s] %s\n", brand.Name, s) },
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
	var name string
	if ra.Name != "" {
		if name, err = ValidateName(ra.Name); err != nil {
			return fail(err)
		}
	} else {
		name = InferName(cwd)
	}
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	code, err := runner.Run(runner.Options{
		Hostname: Hostname(name),
		Args:     ra.Command,
		Force:    ra.Force,
		PortHint: ra.PortHint,
		Daemon:   so,
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
	printRoutes(os.Stdout, resp.Routes, ping.ProxyPort)
	return 0
}

func printRoutes(w io.Writer, routes []proxy.Route, proxyPort int) {
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
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", runner.PublicURL(r.Hostname, proxyPort), target, r.PID, up, r.Command)
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
		so, err := startOptions()
		if err != nil {
			return fail(err)
		}
		r, err := daemon.Ensure(so)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("Proxy running on port %d (pid %d).\n", r.ProxyPort, r.PID)
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
		fmt.Printf("Proxy running on port %d (pid %d), %d route(s).\nLog: %s\n", r.ProxyPort, r.PID, len(list.Routes), daemon.LogPath(dir))
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
	port, err := proxyPort()
	if err != nil {
		return fail(err)
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--port" && i+1 < len(args) {
			if port, err = strconv.Atoi(args[i+1]); err != nil {
				return fail(err)
			}
			i++
		}
	}
	s, err := daemon.Start(daemon.Config{
		StateDir:  brand.StateDir(),
		ProxyPort: port,
		Log:       log.New(os.Stderr, "", log.LstdFlags),
	})
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
