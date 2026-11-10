package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	// leafValidity is short because certificates are made on demand and
	// never stored: there is nothing to rotate by hand.
	leafValidity = 30 * 24 * time.Hour
	// leafRenewBefore is how long before expiry a cached certificate is
	// replaced.
	leafRenewBefore = 24 * time.Hour
	// maxCached bounds the cache against clients sending many distinct SNI
	// names. Reaching it just empties the cache.
	maxCached = 1024
	// fallbackName is served when SNI is missing (https://127.0.0.1:1355)
	// or names a host outside the CA's TLDs.
	fallbackName = "localhost"
)

// Issuer makes and caches a certificate per hostname, signed by a CA.
type Issuer struct {
	ca  *CA
	key *ecdsa.PrivateKey // one key for all leaf certificates
	// Now is the clock, replaceable in tests.
	Now func() time.Time

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// NewIssuer returns an issuer for ca.
func NewIssuer(ca *CA) (*Issuer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Issuer{ca: ca, key: key, Now: time.Now, cache: map[string]*tls.Certificate{}}, nil
}

// TLSConfig returns a server config that picks certificates by SNI and
// offers HTTP/2.
func (is *Issuer) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: is.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
		MinVersion:     tls.VersionTLS12,
	}
}

// GetCertificate implements tls.Config.GetCertificate.
func (is *Issuer) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return is.Certificate(hello.ServerName)
}

// Certificate returns a certificate for serverName, issuing one if needed.
// Names the CA may not sign for get the localhost certificate, so the client
// reports a clear name mismatch instead of a handshake failure.
func (is *Issuer) Certificate(serverName string) (*tls.Certificate, error) {
	name := strings.TrimSuffix(strings.ToLower(serverName), ".")
	if !is.allowed(name) {
		name = fallbackName
	}
	now := is.Now()
	is.mu.Lock()
	defer is.mu.Unlock()
	if c, ok := is.cache[name]; ok && now.Before(c.Leaf.NotAfter.Add(-leafRenewBefore)) {
		return c, nil
	}
	c, err := is.issue(name, now)
	if err != nil {
		return nil, err
	}
	if len(is.cache) >= maxCached {
		clear(is.cache)
	}
	is.cache[name] = c
	return c, nil
}

// allowed reports whether name is a DNS name the CA's constraints permit.
func (is *Issuer) allowed(name string) bool {
	if !validDNSName(name) {
		return false
	}
	for _, tld := range is.ca.Cert.PermittedDNSDomains {
		if name == tld || strings.HasSuffix(name, "."+tld) {
			return true
		}
	}
	return false
}

func (is *Issuer) issue(name string, now time.Time) (*tls.Certificate, error) {
	cn := name
	if len(cn) > 64 { // CN is limited to 64 bytes; clients match on SANs anyway
		cn = cn[:64]
	}
	notAfter := now.Add(leafValidity)
	if notAfter.After(is.ca.Cert.NotAfter) {
		notAfter = is.ca.Cert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber: randomSerial(),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{name},
		NotBefore:    now.Add(-clockSkew),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if name == fallbackName {
		tmpl.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, is.ca.Cert, &is.key.PublicKey, is.ca.Key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: is.key, Leaf: leaf}, nil
}

// validDNSName reports whether s is a syntactically valid hostname: dot
// separated labels of letters, digits, '-' and '_', none empty or longer
// than 63 bytes.
func validDNSName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}
