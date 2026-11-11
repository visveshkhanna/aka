package trust

import (
	"errors"
	"os"

	"aka/internal/certs"
)

// isTrusted checks the system bundle that TLS clients (Go, OpenSSL, curl)
// read, not our anchor file: an anchor that was never compiled into the
// bundle trusts nothing.
func isTrusted(ca *certs.CA) bool { return bundleContains(systemBundles, ca) }

func installSystem(ca *certs.CA, o Options) error {
	d, ok := detectDistro(readOSRelease(), lookPath)
	if !ok {
		return errUnknownDistro()
	}
	// Check before changing anything, so a failure leaves no half install.
	if _, err := lookPath(d.Update); err != nil {
		return errNoUpdateTool(d)
	}
	o.say("Adding the CA to %s and running %s.", d.CertDir, d.Update)
	if err := asRoot(o, "mkdir", "-p", d.CertDir); err != nil {
		return err
	}
	if err := asRoot(o, "install", "-m", "0644", certs.CertPath(ca.Dir), anchorPath(d)); err != nil {
		return err
	}
	return asRoot(o, d.Update)
}

func uninstallSystem(ca *certs.CA, o Options) error {
	d, ok := detectDistro(readOSRelease(), lookPath)
	if !ok {
		return errUnknownDistro()
	}
	if _, err := os.Stat(anchorPath(d)); os.IsNotExist(err) {
		return nil
	}
	if _, err := lookPath(d.Update); err != nil {
		return errNoUpdateTool(d)
	}
	o.say("Removing %s and running %s.", anchorPath(d), d.Update)
	if err := asRoot(o, "rm", "-f", anchorPath(d)); err != nil {
		return err
	}
	var args []string
	if d.Name == "debian" {
		// Without --fresh, the hash symlinks in /etc/ssl/certs are left
		// dangling (the bundle itself is rebuilt either way).
		args = []string{"--fresh"}
	}
	if err := asRoot(o, d.Update, args...); err != nil {
		return err
	}
	if isTrusted(ca) {
		return errors.New("the CA is still in the system bundle after removing it")
	}
	return nil
}

func readOSRelease() string {
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
	}
	return ""
}
