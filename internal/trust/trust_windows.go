package trust

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/visveshkhanna/aka/internal/certs"
)

// isTrusted asks the platform verifier (CertGetCertificateChain via Go's
// x509), which honours the user's Root store.
func isTrusted(ca *certs.CA) bool { return verifiesWithSystemRoots(ca) }

// installSystem adds the CA to the current user's Root store. No admin
// rights are needed; Windows asks the user to confirm with its own dialog.
func installSystem(ca *certs.CA, o Options) error {
	o.say("Adding the CA to your Windows certificate store (current user). Windows will ask you to confirm.")
	store, err := openRoot()
	if err != nil {
		return err
	}
	defer windows.CertCloseStore(store, 0)
	ctx, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, &ca.Cert.Raw[0], uint32(len(ca.Cert.Raw)))
	if err != nil {
		return fmt.Errorf("parse CA for the store: %w", err)
	}
	defer windows.CertFreeCertificateContext(ctx)
	if err := windows.CertAddCertificateContextToStore(store, ctx, windows.CERT_STORE_ADD_REPLACE_EXISTING, nil); err != nil {
		return fmt.Errorf("add CA to the Root store (declined?): %w", err)
	}
	return nil
}

// uninstallSystem deletes every copy of the CA (matched by SHA-1 hash, as
// the store indexes it) from the current user's Root store.
func uninstallSystem(ca *certs.CA, o Options) error {
	store, err := openRoot()
	if err != nil {
		return err
	}
	defer windows.CertCloseStore(store, 0)
	sum := sha1.Sum(ca.Cert.Raw)
	blob := windows.CryptHashBlob{Size: uint32(len(sum)), Data: &sum[0]}
	removed := 0
	for {
		ctx, err := windows.CertFindCertificateInStore(store, windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, 0,
			windows.CERT_FIND_SHA1_HASH, unsafe.Pointer(&blob), nil)
		if err != nil || ctx == nil {
			break
		}
		// CertDeleteCertificateFromStore frees ctx, even on failure.
		if err := windows.CertDeleteCertificateFromStore(ctx); err != nil {
			return fmt.Errorf("remove CA from the Root store (declined?): %w", err)
		}
		removed++
	}
	if removed > 0 {
		o.say("Removed the CA from your Windows certificate store.")
	}
	if isTrusted(ca) {
		return errors.New("the CA is still trusted; it may also be in the machine store (remove it with certlm.msc)")
	}
	return nil
}

func openRoot() (windows.Handle, error) {
	name, _ := windows.UTF16PtrFromString("ROOT")
	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0,
		windows.CERT_SYSTEM_STORE_CURRENT_USER, uintptr(unsafe.Pointer(name)))
	if err != nil {
		return 0, fmt.Errorf("open the Root certificate store: %w", err)
	}
	return store, nil
}
