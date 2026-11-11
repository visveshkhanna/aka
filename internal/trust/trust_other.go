//go:build !darwin && !linux && !windows

package trust

import "github.com/visveshkhanna/aka/internal/certs"

func isTrusted(ca *certs.CA) bool                   { return verifiesWithSystemRoots(ca) }
func installSystem(ca *certs.CA, o Options) error   { return ErrUnsupported }
func uninstallSystem(ca *certs.CA, o Options) error { return ErrUnsupported }
