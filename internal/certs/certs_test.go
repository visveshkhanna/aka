package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func newCA(t *testing.T) *CA {
	t.Helper()
	ca, created, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("created = false for a new directory")
	}
	return ca
}

func pool(ca *CA) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Cert)
	return p
}

func TestCreateThenLoad(t *testing.T) {
	dir := t.TempDir()
	ca, created, err := LoadOrCreateCA(dir)
	if err != nil || !created {
		t.Fatalf("create: %v, created=%v", err, created)
	}
	again, created, err := LoadOrCreateCA(dir)
	if err != nil || created {
		t.Fatalf("reload: %v, created=%v", err, created)
	}
	if again.Fingerprint() != ca.Fingerprint() {
		t.Fatal("reload returned a different CA")
	}
	st, err := os.Stat(KeyPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v, want 0600", st.Mode().Perm())
	}
	if !ca.Cert.IsCA || !ca.Cert.MaxPathLenZero {
		t.Fatal("not a path-length-0 CA")
	}
	if !ca.Covers("localhost") || ca.Covers("com") {
		t.Fatalf("constraints = %v", ca.Cert.PermittedDNSDomains)
	}
	if !strings.HasPrefix(ca.CommonName(), "aka local CA ") {
		t.Fatalf("CN = %q", ca.CommonName())
	}
}

func TestConcurrentCreateMakesOneCA(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	fps := make([]string, 8)
	for i := range fps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ca, _, err := LoadOrCreateCA(dir)
			if err != nil {
				t.Error(err)
				return
			}
			fps[i] = ca.Fingerprint()
		}()
	}
	wg.Wait()
	for _, fp := range fps[1:] {
		if fp != fps[0] {
			t.Fatal("concurrent callers got different CAs")
		}
	}
}

func TestLoadCAErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		if _, err := LoadCA(t.TempDir()); !errors.Is(err, ErrNoCA) {
			t.Fatalf("got %v, want ErrNoCA", err)
		}
	})
	t.Run("key only", func(t *testing.T) {
		dir := newCA(t).Dir
		os.Remove(CertPath(dir))
		_, err := LoadCA(dir)
		if err == nil || errors.Is(err, ErrNoCA) {
			t.Fatalf("got %v, want a damaged-CA error", err)
		}
		// A damaged CA must not be replaced behind the user's back.
		if _, _, err := LoadOrCreateCA(dir); err == nil {
			t.Fatal("LoadOrCreateCA replaced a damaged CA")
		}
	})
	t.Run("corrupt key", func(t *testing.T) {
		dir := newCA(t).Dir
		os.WriteFile(KeyPath(dir), []byte("junk"), 0o600)
		if _, err := LoadCA(dir); err == nil || !strings.Contains(err.Error(), "unusable") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("mismatched key", func(t *testing.T) {
		a, b := newCA(t), newCA(t)
		key, _ := os.ReadFile(KeyPath(b.Dir))
		os.WriteFile(KeyPath(a.Dir), key, 0o600)
		if _, err := LoadCA(a.Dir); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestIssuedCertVerifies(t *testing.T) {
	ca := newCA(t)
	is, err := NewIssuer(ca)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"myapp.localhost", "api.myapp.localhost", "localhost", "MyApp.LocalHost."} {
		c, err := is.Certificate(name)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.TrimSuffix(strings.ToLower(name), ".")
		if _, err := c.Leaf.Verify(x509.VerifyOptions{DNSName: want, Roots: pool(ca)}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if len(c.Leaf.ExtKeyUsage) != 1 || c.Leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
			t.Errorf("%s: ext key usage %v", name, c.Leaf.ExtKeyUsage)
		}
		if c.Leaf.NotBefore.After(time.Now().Add(-time.Minute)) {
			t.Errorf("%s: NotBefore is not backdated", name)
		}
	}
}

func TestFallbackCertificate(t *testing.T) {
	ca := newCA(t)
	is, _ := NewIssuer(ca)
	for _, sni := range []string{"", "example.com", "bad_label-.localhost", "evil.localhost.com"} {
		c, err := is.Certificate(sni)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Leaf.DNSNames; len(got) != 1 || got[0] != "localhost" {
			t.Errorf("SNI %q: got %v, want the localhost certificate", sni, got)
		}
	}
	c, _ := is.Certificate("")
	if _, err := c.Leaf.Verify(x509.VerifyOptions{DNSName: "127.0.0.1", Roots: pool(ca)}); err != nil {
		t.Errorf("127.0.0.1: %v", err)
	}
	if _, err := c.Leaf.Verify(x509.VerifyOptions{DNSName: "::1", Roots: pool(ca)}); err != nil {
		t.Errorf("::1: %v", err)
	}
}

// The point of the name constraints: even a certificate signed directly
// with the CA key is rejected for a name outside .localhost.
func TestNameConstraintsRejectOtherNames(t *testing.T) {
	ca := newCA(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	for _, tc := range []struct {
		dns string
		ip  net.IP
	}{{dns: "example.com"}, {dns: "localhost.example.com"}, {ip: net.IPv4(192, 168, 1, 10)}} {
		tmpl := &x509.Certificate{
			SerialNumber: randomSerial(),
			Subject:      pkix.Name{CommonName: "x"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		name := tc.dns
		if tc.ip != nil {
			tmpl.IPAddresses = []net.IP{tc.ip}
			name = tc.ip.String()
		} else {
			tmpl.DNSNames = []string{tc.dns}
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
		if err != nil {
			t.Fatal(err)
		}
		leaf, _ := x509.ParseCertificate(der)
		_, err = leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: pool(ca)})
		var inv x509.CertificateInvalidError
		if !errors.As(err, &inv) || inv.Reason != x509.CANotAuthorizedForThisName {
			t.Errorf("%s: got %v, want CANotAuthorizedForThisName", name, err)
		}
	}
}

func TestCacheAndRenewal(t *testing.T) {
	ca := newCA(t)
	is, _ := NewIssuer(ca)
	now := time.Now()
	is.Now = func() time.Time { return now }
	a, _ := is.Certificate("a.localhost")
	b, _ := is.Certificate("a.localhost")
	if a != b {
		t.Fatal("second call did not use the cache")
	}
	now = a.Leaf.NotAfter.Add(-leafRenewBefore + time.Minute)
	c, _ := is.Certificate("a.localhost")
	if c == a {
		t.Fatal("certificate near expiry was not renewed")
	}
	if !c.Leaf.NotAfter.After(a.Leaf.NotAfter) {
		t.Fatal("renewed certificate does not expire later")
	}
}

func TestCacheIsBounded(t *testing.T) {
	is, _ := NewIssuer(newCA(t))
	for i := range maxCached + 5 {
		if _, err := is.Certificate("h" + strconv.Itoa(i) + ".localhost"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(is.cache); n > maxCached {
		t.Fatalf("cache has %d entries, max %d", n, maxCached)
	}
}

func TestHandshake(t *testing.T) {
	ca := newCA(t)
	is, _ := NewIssuer(ca)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", is.TLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				c.(*tls.Conn).Handshake()
				io.WriteString(c, "hi")
				c.Close()
			}()
		}
	}()
	for _, proto := range []string{"h2", "http/1.1"} {
		c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
			ServerName: "myapp.localhost",
			RootCAs:    pool(ca),
			NextProtos: []string{proto},
		})
		if err != nil {
			t.Fatalf("%s: %v", proto, err)
		}
		if got := c.ConnectionState().NegotiatedProtocol; got != proto {
			t.Errorf("ALPN = %q, want %q", got, proto)
		}
		c.Close()
	}
	// A client that trusts nothing extra must fail verification.
	if c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{ServerName: "myapp.localhost", RootCAs: x509.NewCertPool()}); err == nil {
		c.Close()
		t.Fatal("handshake succeeded without trusting the CA")
	}
}

func TestValidDNSName(t *testing.T) {
	for s, want := range map[string]bool{
		"a.localhost": true, "a-b.c_d.localhost": true, "localhost": true,
		"": false, "a..localhost": false, "-a.localhost": false, "a b.localhost": false,
		strings.Repeat("a", 64) + ".localhost": false, "*.localhost": false,
	} {
		if got := validDNSName(s); got != want {
			t.Errorf("validDNSName(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestWriteFileAtomicLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := writeFileAtomic(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("dir has %d entries, want 1", len(ents))
	}
}
