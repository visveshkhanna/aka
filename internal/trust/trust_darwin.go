package trust

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/visveshkhanna/aka/internal/certs"
)

func isTrusted(ca *certs.CA) bool { return verifiesWithSystemRoots(ca) }

// installSystem trusts ca for SSL in the user's login keychain. macOS shows
// an authorization dialog; no sudo is needed.
func installSystem(ca *certs.CA, o Options) error {
	o.say("Adding the CA to your login keychain. macOS will ask you to confirm.")
	return runner("security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", loginKeychain(), certs.CertPath(ca.Dir))
}

// uninstallSystem removes the trust setting, then the certificate itself.
func uninstallSystem(ca *certs.CA, o Options) error {
	o.say("Removing the CA from your login keychain. macOS may ask you to confirm.")
	// Fails if there is no trust setting; the delete below still runs.
	trustErr := runner("security", "remove-trusted-cert", certs.CertPath(ca.Dir))
	sum := sha1.Sum(ca.Cert.Raw)
	delErr := runner("security", "delete-certificate", "-Z", strings.ToUpper(hex.EncodeToString(sum[:])), loginKeychain())
	if isTrusted(ca) {
		if trustErr != nil {
			return trustErr
		}
		return delErr
	}
	return nil
}

func loginKeychain() string {
	if out, err := exec.Command("security", "default-keychain", "-d", "user").Output(); err == nil {
		if p := strings.Trim(strings.TrimSpace(string(out)), `"`); p != "" {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Keychains", "login.keychain-db")
}
