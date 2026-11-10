package trust

import (
	"os"

	"aka/internal/certs"
)

func isTrusted(ca *certs.CA) bool {
	d, ok := detectDistro(readOSRelease(), lookPath)
	return ok && anchorInstalled(ca, d)
}

func installSystem(ca *certs.CA, o Options) error {
	d, ok := detectDistro(readOSRelease(), lookPath)
	if !ok {
		return errUnknownDistro()
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
	o.say("Removing %s and running %s.", anchorPath(d), d.Update)
	if err := asRoot(o, "rm", "-f", anchorPath(d)); err != nil {
		return err
	}
	return asRoot(o, d.Update)
}

func readOSRelease() string {
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
	}
	return ""
}
