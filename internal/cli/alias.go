package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/daemon"
)

// cmdAlias manages static routes:
//
//	aka alias                     list aliases
//	aka alias <name> <port>       route <name>.localhost to a port aka did not start
//	aka alias <name> <host:port>  same, dialling that loopback address
//	aka alias rm <name>           remove it
func cmdAlias(args []string) int {
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	switch {
	case len(args) == 0:
		return listAliases(so)
	case len(args) == 2 && (args[0] == "rm" || args[0] == "remove"):
		name, err := ValidateName(args[1])
		if err != nil {
			return fail(err)
		}
		r, err := daemon.Do(so.StateDir, daemon.Request{Op: daemon.OpAlias, Hostname: Hostname(name), Remove: true})
		if err != nil {
			return fail(errors.New("the proxy is not running, so there are no aliases to remove"))
		}
		if !r.OK {
			return fail(errors.New(r.Error))
		}
		fmt.Printf("Removed %s.\n", Hostname(name))
		return 0
	case len(args) == 2:
		name, err := ValidateName(args[0])
		if err != nil {
			return fail(err)
		}
		host, port, err := parseTarget(args[1])
		if err != nil {
			return fail(err)
		}
		if so.TLS {
			ensureTrust(so.StateDir)
		}
		ping, err := daemon.Ensure(so)
		if err != nil {
			return fail(err)
		}
		r, err := daemon.Do(so.StateDir, daemon.Request{Op: daemon.OpAlias, Hostname: Hostname(name), Port: port, DialHost: host})
		if err != nil {
			return fail(err)
		}
		if !r.OK {
			return fail(errors.New(r.Error))
		}
		fmt.Printf("%s -> %s\n", ping.URL(Hostname(name)), net.JoinHostPort(orLocal(host), strconv.Itoa(port)))
		return 0
	}
	return fail(fmt.Errorf("usage: %[1]s alias [<name> <port> | rm <name>]", brand.Name))
}

// parseTarget accepts "5432", "127.0.0.1:5432" or "[::1]:5432". Only
// loopback addresses are allowed: the proxy is for local services.
func parseTarget(s string) (string, int, error) {
	host, ps := "", s
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, ps = h, p
	}
	port, err := strconv.Atoi(ps)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid port %q", s)
	}
	switch host {
	case "", "localhost":
		return "", port, nil
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", 0, fmt.Errorf("%s is not a loopback address; aliases point at local services", host)
	}
	return host, port, nil
}

func orLocal(h string) string {
	if h == "" {
		return "localhost"
	}
	return h
}

func listAliases(so daemon.StartOptions) int {
	r, err := daemon.Do(so.StateDir, daemon.Request{Op: daemon.OpList})
	if err != nil {
		fmt.Println("No aliases (the proxy is not running).")
		return 0
	}
	ping, _ := daemon.Ping(so.StateDir)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	n := 0
	for _, rt := range r.Routes {
		if rt.PID != 0 {
			continue
		}
		if n == 0 {
			fmt.Fprintln(tw, "URL\tTARGET")
		}
		n++
		fmt.Fprintf(tw, "%s\t%s\n", ping.URL(rt.Hostname), net.JoinHostPort(orLocal(rt.DialHost), strconv.Itoa(rt.Port)))
	}
	tw.Flush()
	if n == 0 {
		fmt.Printf("No aliases. Add one with: %s alias <name> <port>\n", brand.Name)
	}
	return 0
}
