package trust

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"aka/internal/brand"
	"aka/internal/certs"
)

// distro is where a Linux family keeps extra CA anchors and how it rebuilds
// the system bundle from them.
type distro struct {
	Name    string
	CertDir string
	Update  string
}

var (
	debian = distro{"debian", "/usr/local/share/ca-certificates", "update-ca-certificates"}
	fedora = distro{"fedora", "/etc/pki/ca-trust/source/anchors", "update-ca-trust"}
	arch   = distro{"arch", "/etc/ca-certificates/trust-source/anchors", "update-ca-trust"}
	suse   = distro{"suse", "/etc/pki/trust/anchors", "update-ca-certificates"}
)

// families maps os-release IDs to a layout. Alpine uses the Debian layout.
var families = map[string]distro{
	"debian": debian, "ubuntu": debian, "alpine": debian, "linuxmint": debian, "pop": debian,
	"fedora": fedora, "rhel": fedora, "centos": fedora, "rocky": fedora, "almalinux": fedora, "amzn": fedora,
	"arch": arch, "manjaro": arch, "endeavouros": arch,
	"suse": suse, "opensuse": suse, "opensuse-leap": suse, "opensuse-tumbleweed": suse, "sles": suse,
}

// detectDistro picks a layout from os-release content: ID first, then each
// ID_LIKE entry. Failing that, the first layout whose update command is
// installed.
func detectDistro(osRelease string, look func(string) (string, error)) (distro, bool) {
	fields := parseOSRelease(osRelease)
	ids := append([]string{fields["ID"]}, strings.Fields(fields["ID_LIKE"])...)
	for _, id := range ids {
		if d, ok := families[id]; ok {
			return d, true
		}
	}
	for _, d := range []distro{debian, fedora, arch, suse} {
		if _, err := look(d.Update); err == nil {
			return d, true
		}
	}
	return distro{}, false
}

func parseOSRelease(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		m[k] = strings.ToLower(strings.Trim(v, `"'`))
	}
	return m
}

// anchorPath is our CA's file in the distro's anchor directory. Debian's
// update-ca-certificates only reads *.crt.
func anchorPath(d distro) string {
	return filepath.Join(d.CertDir, brand.Name+"-local-ca.crt")
}

func anchorInstalled(ca *certs.CA, d distro) bool {
	b, err := os.ReadFile(anchorPath(d))
	return err == nil && bytes.Equal(bytes.TrimSpace(b), bytes.TrimSpace(ca.CertPEM))
}

func errUnknownDistro() error {
	return errors.New("could not tell which Linux CA store to use (no known ID in /etc/os-release and no update-ca-certificates or update-ca-trust)")
}
