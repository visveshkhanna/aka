// Package service installs the proxy daemon as a per-user service that
// starts at login: a launchd agent on macOS, a systemd user unit on Linux,
// and a Run key entry on Windows. No administrator rights are needed.
//
// The service restarts the daemon only if it crashes, so `aka proxy stop`
// still stops it until the next login.
package service

import (
	"errors"
	"fmt"
	"html"
	"os"
	"sort"
	"strings"

	"aka/internal/brand"
)

// ErrUnsupported is returned where no service manager is supported.
var ErrUnsupported = errors.New("installing a login service is not supported on this platform")

// Spec describes the daemon to run.
type Spec struct {
	Exe     string            // absolute path of the aka binary
	Args    []string          // daemon arguments (__daemon --port ...)
	Env     map[string]string // AKA_* settings to keep
	LogPath string
}

// Label is the service's name in the service manager.
var Label = "local." + brand.Name + ".proxy"

// unitName is the systemd unit's file name.
var unitName = brand.Name + "-proxy.service"

// Status is what the service manager reports.
type Status struct {
	Installed bool
	Running   bool
	Path      string // the plist, unit file or registry value
	Detail    string
}

// Install writes the service definition and starts it.
func Install(s Spec) (string, error) { return install(s) }

// Uninstall stops the service and removes its definition.
func Uninstall() error { return uninstall() }

// Query reports whether the service is installed and running.
func Query() Status { return query() }

// Plist is the launchd agent definition.
func Plist(s Spec) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + esc(Label) + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range append([]string{s.Exe}, s.Args...) {
		b.WriteString("\t\t<string>" + esc(a) + "</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>` + esc(s.LogPath) + `</string>
	<key>StandardErrorPath</key>
	<string>` + esc(s.LogPath) + `</string>
`)
	if len(s.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		for _, k := range sortedKeys(s.Env) {
			b.WriteString("\t\t<key>" + esc(k) + "</key>\n\t\t<string>" + esc(s.Env[k]) + "</string>\n")
		}
		b.WriteString("\t</dict>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// Unit is the systemd user unit.
func Unit(s Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=%s local dev URL proxy\n\n[Service]\nType=simple\n", brand.Name)
	for _, k := range sortedKeys(s.Env) {
		fmt.Fprintf(&b, "Environment=%s\n", systemdQuote(k+"="+s.Env[k]))
	}
	words := []string{systemdQuote(s.Exe)}
	for _, a := range s.Args {
		words = append(words, systemdQuote(a))
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(words, " "))
	// Restart after a crash, not after `aka proxy stop` (exit 0).
	b.WriteString("Restart=on-failure\nRestartSec=2\n")
	fmt.Fprintf(&b, "StandardOutput=append:%s\nStandardError=append:%s\n", s.LogPath, s.LogPath)
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// systemdQuote quotes a word for ExecStart/Environment when needed.
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$%;") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `$$`, `%`, `%%`)
	return `"` + r.Replace(s) + `"`
}

// RunCommand is the Windows Run key value: a hidden PowerShell that runs
// `aka proxy start`, which starts the daemon detached and exits, so no
// console window stays open.
func RunCommand(s Spec) string {
	q := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	var set []string
	for _, k := range sortedKeys(s.Env) {
		set = append(set, "$env:"+k+"="+q(s.Env[k])+"; ")
	}
	return `powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -Command "` +
		strings.Join(set, "") + "& " + q(s.Exe) + ` proxy start"`
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeFile(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
