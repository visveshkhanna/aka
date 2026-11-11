package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/certs"
	"github.com/visveshkhanna/aka/internal/daemon"
	"github.com/visveshkhanna/aka/internal/privdrop"
	"github.com/visveshkhanna/aka/internal/trust"
)

// httpsEnabled reports whether HTTPS is on. It is unless AKA_HTTPS is set
// to 0, false, off or no.
func httpsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(brand.Env("HTTPS")))) {
	case "0", "false", "off", "no":
		return false
	}
	return true
}

// httpsPort is the dedicated HTTPS port to try: AKA_HTTPS_PORT, default
// 443. 0 means none.
func httpsPort() (int, error) {
	v := os.Getenv(brand.Env("HTTPS_PORT"))
	if v == "" {
		return brand.DefaultHTTPSPort, nil
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 0 || p > 65535 {
		return 0, fmt.Errorf("%s: invalid port %q", brand.Env("HTTPS_PORT"), v)
	}
	return p, nil
}

// trustAskedFile records the CA fingerprint we last offered to trust, so
// the automatic prompt happens once per CA, not on every run.
const trustAskedFile = "trust-asked"

// ensureTrust makes sure the local CA exists and, the first time, offers
// to add it to the system trust store. Later runs with an untrusted CA get
// a one-line hint instead. Problems here never stop the app from running.
func ensureTrust(dir string) {
	ca, _, err := certs.LoadOrCreateCA(dir)
	if err != nil {
		notice("%v", err)
		return
	}
	if trust.IsTrusted(ca) {
		return
	}
	marker := filepath.Join(dir, trustAskedFile)
	asked, _ := os.ReadFile(marker)
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
	if !interactive || strings.TrimSpace(string(asked)) == ca.Fingerprint() {
		notice("HTTPS certificates are not trusted by this system yet; run `%s trust` (or set %s=0 for plain HTTP).", brand.Name, brand.Env("HTTPS"))
		return
	}
	os.WriteFile(marker, []byte(ca.Fingerprint()+"\n"), 0o600)
	notice("First HTTPS run: adding %s's local CA to the system trust store so browsers accept https://*.%s.", brand.Name, brand.DefaultTLD)
	notice("The CA can only sign *.%s and loopback names. Undo any time with `%s untrust`.", brand.DefaultTLD, brand.Name)
	if err := trust.Install(ca, trust.Options{Out: os.Stderr}); err != nil {
		notice("Could not trust the CA (%v). Run `%s trust` to try again.", err, brand.Name)
		return
	}
	if !trust.IsTrusted(ca) {
		notice("The CA was added but the system does not report it as trusted yet; see `%s trust`.", brand.Name)
	}
}

func notice(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "[%s] %s\n", brand.Name, fmt.Sprintf(format, a...))
}

func cmdTrust() int {
	dir := brand.StateDir()
	ca, created, err := certs.LoadOrCreateCA(dir)
	if err != nil {
		return fail(err)
	}
	if created {
		fmt.Printf("Created the local CA: %s\n", certs.CertPath(dir))
	}
	if trust.IsTrusted(ca) {
		fmt.Println("The local CA is already trusted by the system.")
		return 0
	}
	if err := trust.Install(ca, trust.Options{Out: os.Stdout}); err != nil {
		return fail(err)
	}
	os.WriteFile(filepath.Join(dir, trustAskedFile), []byte(ca.Fingerprint()+"\n"), 0o600)
	if !trust.IsTrusted(ca) {
		return fail(errors.New("the CA was added, but the system still does not trust it"))
	}
	fmt.Printf("Trusted. Browsers now accept https://*.%s from %s (restart a browser that was already open).\n", brand.DefaultTLD, brand.Name)
	return 0
}

func cmdUntrust() int {
	ca, err := certs.LoadCA(brand.StateDir())
	if errors.Is(err, certs.ErrNoCA) {
		fmt.Println("There is no local CA, so nothing to remove.")
		return 0
	}
	if err != nil {
		return fail(err)
	}
	if err := trust.Uninstall(ca, trust.Options{Out: os.Stdout}); err != nil {
		return fail(err)
	}
	fmt.Println("The local CA is no longer trusted. Its files remain in " + brand.StateDir() + ".")
	return 0
}

// startPrivileged handles `sudo aka proxy start`: bind the HTTPS port as
// root, drop to the user who ran sudo, then start the daemon as that user
// with the bound sockets passed in. Nothing else ever runs as root.
func startPrivileged() int {
	u, ok := privdrop.SudoUser()
	if !ok {
		notice("running as root without sudo: the proxy and its files will belong to root.")
		return startProxy(nil)
	}
	port, err := httpsPort()
	if err != nil {
		return fail(err)
	}
	if !httpsEnabled() || port == 0 {
		return fail(fmt.Errorf("sudo is only needed for the HTTPS port, which is disabled (%s, %s)", brand.Env("HTTPS"), brand.Env("HTTPS_PORT")))
	}
	files, err := bindFiles(port)
	if err != nil {
		return fail(err)
	}
	if err := privdrop.To(u); err != nil {
		return fail(fmt.Errorf("could not drop root privileges: %w", err))
	}
	// From here on we are the user, and brand.StateDir() is theirs.
	dir := brand.StateDir()
	if r, err := daemon.Ping(dir); err == nil {
		if r.HTTPSPort == port {
			fmt.Printf("Proxy already serves HTTPS on port %d (pid %d).\n", port, r.PID)
			return 0
		}
		if !daemon.Idle(dir) {
			return fail(fmt.Errorf("the proxy is running without port %d and has apps attached; stop them, then run this again", port))
		}
		if err := daemon.Stop(dir, 5*time.Second); err != nil {
			return fail(err)
		}
	}
	return startProxy(files)
}

// bindFiles binds port on the loopback addresses and returns the sockets as
// files, to pass to the daemon. ::1 is skipped where IPv6 is unavailable.
func bindFiles(port int) ([]*os.File, error) {
	var files []*os.File
	for i, h := range []string{"127.0.0.1", "::1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, strconv.Itoa(port)))
		if err != nil {
			if i > 0 {
				continue
			}
			return nil, fmt.Errorf("bind %s port %d: %w", h, port, err)
		}
		f, err := ln.(*net.TCPListener).File()
		ln.Close() // f is a duplicate and keeps the socket open
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// startProxy ensures the daemon runs, passing files as pre-bound HTTPS
// listeners when given, and prints where it serves.
func startProxy(files []*os.File) int {
	so, err := startOptions()
	if err != nil {
		return fail(err)
	}
	if len(files) > 0 {
		so.ExtraFiles = files
		fds := make([]string, len(files))
		for i := range files {
			fds[i] = strconv.Itoa(3 + i)
		}
		so.Args = append(so.Args, "--https-fds", strings.Join(fds, ","))
	}
	if so.TLS {
		ensureTrust(so.StateDir)
	}
	r, err := daemon.Ensure(so)
	if err != nil {
		return fail(err)
	}
	fmt.Println(describeProxy(r))
	if note := daemon.HTTPSNote(r); note != "" {
		fmt.Println(note)
	}
	return 0
}

// describeProxy is a one-line summary of where the daemon serves.
func describeProxy(r daemon.Response) string {
	s := fmt.Sprintf("Proxy running (pid %d): ", r.PID)
	switch {
	case !r.TLS:
		s += fmt.Sprintf("HTTP on port %d.", r.ProxyPort)
	case r.HTTPSPort > 0:
		s += fmt.Sprintf("HTTPS on port %d, HTTP and HTTPS on port %d.", r.HTTPSPort, r.ProxyPort)
	default:
		s += fmt.Sprintf("HTTP and HTTPS on port %d.", r.ProxyPort)
	}
	return s
}
