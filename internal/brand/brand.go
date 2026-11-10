// Package brand holds every user-visible name the tool uses. "aka" is a
// working name: renaming the tool means editing this file (and the cmd/
// directory name) and nothing else.
package brand

import (
	"os"
	"path/filepath"
)

const (
	// Name is the binary name, used in help text and messages.
	Name = "aka"

	// EnvPrefix prefixes every environment variable the tool reads or sets.
	EnvPrefix = "AKA_"

	// StateDirName is the per-user state directory, created under $HOME.
	StateDirName = ".aka"

	// DefaultTLD is appended to route names. .localhost resolves to loopback
	// without touching /etc/hosts (RFC 6761) in browsers and most resolvers.
	DefaultTLD = "localhost"

	// DefaultProxyPort is the unprivileged HTTP proxy port.
	DefaultProxyPort = 1355
	// DefaultHTTPSPort is tried for HTTPS so URLs need no port. Without the
	// privilege to bind it, HTTPS is served on DefaultProxyPort instead.
	DefaultHTTPSPort = 443

	// ProxyHeader is set on every response the proxy generates or forwards,
	// so health checks can tell our proxy apart from an unrelated server.
	ProxyHeader = "X-Aka"

	// HopsHeader counts how many times a request passed through the proxy,
	// to detect forwarding loops.
	HopsHeader = "X-Aka-Hops"
)

// Env returns the full name of an environment variable, e.g. Env("URL") is
// "AKA_URL".
func Env(suffix string) string { return EnvPrefix + suffix }

// StateDir returns the state directory. AKA_STATE_DIR overrides it, which
// tests use to run isolated daemons.
func StateDir() string {
	if d := os.Getenv(Env("STATE_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, StateDirName)
}
