// Package certs is aka's local certificate authority. It creates a CA once
// per state directory and issues a certificate for each hostname on demand
// during the TLS handshake (SNI), so every <name>.localhost gets a valid
// certificate without wildcards.
//
// The CA is name-constrained: it can only sign certificates for the
// development TLDs (localhost) and loopback IPs. Trusting it therefore cannot
// be abused to impersonate real sites, even if its key leaks.
package certs

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/lockfile"
)

// File names in the state directory.
const (
	CACertFile = "ca.pem"
	CAKeyFile  = "ca-key.pem"
	caLockFile = "ca.lock"
)

const (
	caValidity = 10 * 365 * 24 * time.Hour
	// clockSkew backdates NotBefore so a certificate is valid immediately on
	// a machine whose clock is slightly behind (VMs, containers).
	clockSkew = 5 * time.Minute
)

// LoopbackNets are the IP ranges the CA may sign for.
var LoopbackNets = []*net.IPNet{
	{IP: net.IPv4(127, 0, 0, 0).To4(), Mask: net.CIDRMask(8, 32)},
	{IP: net.IPv6loopback, Mask: net.CIDRMask(128, 128)},
}

// CA is a loaded certificate authority.
type CA struct {
	Cert    *x509.Certificate
	Key     crypto.Signer
	CertPEM []byte
	Dir     string
}

// ErrNoCA is returned by LoadCA when the state directory has no CA yet.
var ErrNoCA = errors.New("no local CA yet")

// CertPath is the CA certificate's path in dir. This is the file to trust
// and the value for NODE_EXTRA_CA_CERTS.
func CertPath(dir string) string { return filepath.Join(dir, CACertFile) }

// KeyPath is the CA private key's path in dir.
func KeyPath(dir string) string { return filepath.Join(dir, CAKeyFile) }

// LoadOrCreateCA loads the CA in dir, creating it if there is none. It is
// safe to call from several processes at once: creation happens under a
// file lock, so exactly one CA is ever made.
func LoadOrCreateCA(dir string) (ca *CA, created bool, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, err
	}
	unlock, err := lockfile.Lock(filepath.Join(dir, caLockFile))
	if err != nil {
		return nil, false, fmt.Errorf("lock CA: %w", err)
	}
	defer unlock()

	ca, err = LoadCA(dir)
	if !errors.Is(err, ErrNoCA) {
		return ca, false, err
	}
	if err := createCA(dir, brand.DefaultTLD); err != nil {
		return nil, false, err
	}
	ca, err = LoadCA(dir)
	return ca, err == nil, err
}

// LoadCA loads the CA in dir. It returns ErrNoCA if neither file exists, and
// a descriptive error if the files are incomplete or damaged: a broken CA is
// never silently replaced, since that would invalidate the trusted one.
func LoadCA(dir string) (*CA, error) {
	certPEM, certErr := os.ReadFile(CertPath(dir))
	keyPEM, keyErr := os.ReadFile(KeyPath(dir))
	switch {
	case errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist):
		return nil, ErrNoCA
	case certErr != nil:
		return nil, damaged(dir, certErr)
	case keyErr != nil:
		return nil, damaged(dir, keyErr)
	}
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, damaged(dir, err)
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return nil, damaged(dir, err)
	}
	if !cert.IsCA {
		return nil, damaged(dir, errors.New("certificate is not a CA"))
	}
	if !publicKeysEqual(cert.PublicKey, key.Public()) {
		return nil, damaged(dir, errors.New("key does not match certificate"))
	}
	return &CA{Cert: cert, Key: key, CertPEM: certPEM, Dir: dir}, nil
}

func damaged(dir string, err error) error {
	return fmt.Errorf("local CA in %s is unusable (%v). To start over, run `%s untrust` and delete %s and %s",
		dir, err, brand.Name, CACertFile, CAKeyFile)
}

// Fingerprint is the hex SHA-256 of the CA certificate.
func (ca *CA) Fingerprint() string {
	sum := sha256.Sum256(ca.Cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Covers reports whether the CA's name constraints allow certificates for
// names under tld.
func (ca *CA) Covers(tld string) bool {
	return slices.Contains(ca.Cert.PermittedDNSDomains, tld)
}

// CommonName is the CA's subject CN, used to find it in OS trust stores.
func (ca *CA) CommonName() string { return ca.Cert.Subject.CommonName }

func createCA(dir string, tlds ...string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(),
		Subject: pkix.Name{
			Organization:       []string{brand.Name + " development CA"},
			OrganizationalUnit: []string{userAndHost()},
			CommonName:         brand.Name + " local CA " + userAndHost(),
		},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		// RFC 5280 requires name constraints to be critical. Clients that
		// cannot enforce them must reject the CA rather than ignore them.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         tlds,
		PermittedIPRanges:           LoopbackNets,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	// Key first: a crash between the two writes leaves a key without a
	// certificate, which LoadCA reports instead of half-trusting anything.
	if err := writeFileAtomic(KeyPath(dir), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return writeFileAtomic(CertPath(dir), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func userAndHost() string {
	name := "user"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	if host == "" {
		return name
	}
	return name + "@" + host
}

func randomSerial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return n.Add(n, big.NewInt(1)) // serials must be positive
}

func parseCertPEM(b []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("no PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKeyPEM(b []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("no PEM key")
	}
	var key any
	var err error
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type %q", block.Type)
	}
	if err != nil {
		return nil, err
	}
	s, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("key cannot sign")
	}
	return s, nil
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	ea, err1 := x509.MarshalPKIXPublicKey(a)
	eb, err2 := x509.MarshalPKIXPublicKey(b)
	return err1 == nil && err2 == nil && bytes.Equal(ea, eb)
}

// writeFileAtomic writes data to a temp file in the same directory and
// renames it into place, so readers never see a partial file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
