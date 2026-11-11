package trust

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/visveshkhanna/aka/internal/certs"
)

// nssDBs lists NSS certificate databases under home: Chrome/Chromium's on
// Linux and every Firefox profile. Each entry is a certutil -d argument.
func nssDBs(home string) []string {
	var dirs []string
	if runtime.GOOS == "linux" {
		dirs = append(dirs,
			filepath.Join(home, ".pki", "nssdb"),
			filepath.Join(home, "snap", "chromium", "current", ".pki", "nssdb"))
	}
	for _, pattern := range []string{
		filepath.Join(home, ".mozilla", "firefox", "*"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "*"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox", "*"),
		filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*"),
		filepath.Join(home, "AppData", "Roaming", "Mozilla", "Firefox", "Profiles", "*"),
	} {
		m, _ := filepath.Glob(pattern)
		dirs = append(dirs, m...)
	}
	var dbs []string
	for _, d := range dirs {
		if fileExists(filepath.Join(d, "cert9.db")) {
			dbs = append(dbs, "sql:"+d)
		} else if fileExists(filepath.Join(d, "cert8.db")) {
			dbs = append(dbs, "dbm:"+d)
		}
	}
	return dbs
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// installNSS adds ca to every NSS database found. Failures are reported but
// not fatal: the system store is what matters most.
func installNSS(ca *certs.CA, o Options) {
	home, _ := os.UserHomeDir()
	dbs := nssDBs(home)
	if len(dbs) == 0 {
		return
	}
	certutil, err := lookPath("certutil")
	if err != nil {
		o.say("Firefox (and Chrome on Linux) keep their own certificate store. To trust the CA there too, install certutil (Debian/Ubuntu: libnss3-tools, Fedora: nss-tools, macOS: brew install nss) and run this again.")
		return
	}
	for _, db := range dbs {
		if err := runner(certutil, "-A", "-d", db, "-t", "C,,", "-n", nickname(ca), "-i", certs.CertPath(ca.Dir)); err != nil {
			o.say("Could not add the CA to %s: %v", db, err)
		} else {
			o.say("Added the CA to %s", db)
		}
	}
}

func uninstallNSS(ca *certs.CA, o Options) {
	home, _ := os.UserHomeDir()
	certutil, err := lookPath("certutil")
	if err != nil {
		return
	}
	for _, db := range nssDBs(home) {
		if !probe(certutil, "-L", "-d", db, "-n", nickname(ca)) {
			continue // never added here
		}
		if err := runner(certutil, "-D", "-d", db, "-n", nickname(ca)); err != nil {
			o.say("Could not remove the CA from %s: %v", db, err)
		} else {
			o.say("Removed the CA from %s", db)
		}
	}
}
