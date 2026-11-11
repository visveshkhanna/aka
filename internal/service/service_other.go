//go:build !darwin && !linux && !windows

package service

func install(Spec) (string, error) { return "", ErrUnsupported }
func uninstall() error             { return ErrUnsupported }
func query() Status                { return Status{} }

const startsOnInstall = false
