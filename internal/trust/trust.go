// Package trust adds the local CA to the operating system's trust store, so
// browsers and system TLS clients accept https://<name>.localhost.
//
// macOS: the login keychain, via `security` (the OS asks for confirmation).
// Linux: the distro's CA anchor directory plus its update command, via sudo.
// Both: NSS databases (Chrome and Firefox on Linux, Firefox profiles) when
// certutil is installed.
package trust

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"aka/internal/brand"
	"aka/internal/certs"
)

// ErrUnsupported is returned on platforms without trust store support yet.
var ErrUnsupported = errors.New("trust store install is not supported on this platform yet")

// Options control an install or uninstall.
type Options struct {
	// Out receives a line for each step taken.
	Out io.Writer
}

func (o Options) say(format string, a ...any) {
	if o.Out != nil {
		fmt.Fprintf(o.Out, format+"\n", a...)
	}
}

// IsTrusted reports whether the system trust store accepts certificates
// from ca for TLS servers.
func IsTrusted(ca *certs.CA) bool { return isTrusted(ca) }

// Install adds ca to the system trust store and any NSS databases found. It
// may prompt: a macOS authorization dialog, or sudo's password prompt.
func Install(ca *certs.CA, o Options) error {
	if err := installSystem(ca, o); err != nil {
		return err
	}
	installNSS(ca, o)
	return nil
}

// Uninstall removes ca from the system trust store and NSS databases.
func Uninstall(ca *certs.CA, o Options) error {
	uninstallNSS(ca, o)
	return uninstallSystem(ca, o)
}

// verifiesWithSystemRoots checks a fresh leaf from ca against the platform
// verifier (x509 with nil Roots). On macOS this is the Security framework,
// which applies the user's keychain trust settings.
func verifiesWithSystemRoots(ca *certs.CA) bool {
	is, err := certs.NewIssuer(ca)
	if err != nil {
		return false
	}
	name := brand.Name + "-trust-check." + brand.DefaultTLD
	c, err := is.Certificate(name)
	if err != nil {
		return false
	}
	_, err = c.Leaf.Verify(x509.VerifyOptions{DNSName: name, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err == nil
}

// runner executes external commands; tests replace it.
var runner = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return nil
}

// probe runs a command silently and reports whether it succeeded; tests
// replace it.
var probe = func(name string, args ...string) bool {
	return exec.Command(name, args...).Run() == nil
}

// lookPath finds commands; tests replace it.
var lookPath = exec.LookPath

// isRoot reports whether we run as root; tests replace it.
var isRoot = func() bool { return os.Geteuid() == 0 }

// asRoot runs a command as root: directly when we are root, else via sudo.
func asRoot(o Options, name string, args ...string) error {
	if isRoot() {
		return runner(name, args...)
	}
	if _, err := lookPath("sudo"); err != nil {
		return fmt.Errorf("this step needs root and sudo is not installed; run as root: %s %s", name, strings.Join(args, " "))
	}
	o.say("Running with sudo: %s %s", name, strings.Join(args, " "))
	return runner("sudo", append([]string{"--", name}, args...)...)
}

// nickname names the CA in stores that index certificates by a label.
func nickname(ca *certs.CA) string { return ca.CommonName() }
