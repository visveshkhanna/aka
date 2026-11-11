package service

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

var valueName = Label

const startsOnInstall = false

func install(s Spec) (string, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	if err := k.SetStringValue(valueName, RunCommand(s)); err != nil {
		return "", err
	}
	return `HKCU\` + runKey + `\` + valueName, nil
}

func uninstall() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func query() Status {
	st := Status{Path: `HKCU\` + runKey + `\` + valueName}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return st
	}
	defer k.Close()
	if v, _, err := k.GetStringValue(valueName); err == nil {
		st.Installed, st.Detail = true, v
	}
	return st // Running is answered by pinging the daemon instead
}
