package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var spec = Spec{
	Exe:     "/Users/me/My Tools/aka",
	Args:    []string{"__daemon", "--port", "1355", "--https-port", "443"},
	Env:     map[string]string{"AKA_STATE_DIR": "/tmp/a&b", "AKA_PROXY_PORT": "1355"},
	LogPath: "/Users/me/.aka/daemon.log",
}

func TestPlist(t *testing.T) {
	p := Plist(spec)
	for _, want := range []string{
		"<string>local.aka.proxy</string>",
		"<string>/Users/me/My Tools/aka</string>",
		"<string>__daemon</string>",
		"<key>SuccessfulExit</key>\n\t\t<false/>",
		"<string>/tmp/a&amp;b</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q:\n%s", want, p)
		}
	}
	if runtime.GOOS == "darwin" {
		f := filepath.Join(t.TempDir(), "x.plist")
		os.WriteFile(f, []byte(p), 0o644)
		if out, err := exec.Command("plutil", "-lint", f).CombinedOutput(); err != nil {
			t.Errorf("plutil -lint: %v %s", err, out)
		}
	}
}

func TestUnit(t *testing.T) {
	u := Unit(spec)
	for _, want := range []string{
		`ExecStart="/Users/me/My Tools/aka" __daemon --port 1355 --https-port 443`,
		"Environment=AKA_PROXY_PORT=1355",
		"Environment=AKA_STATE_DIR=/tmp/a&b", // & is not special to systemd
		"Restart=on-failure",
		"StandardError=append:/Users/me/.aka/daemon.log",
		"WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
	if q := systemdQuote(`a "b" $x 50%`); q != `"a \"b\" $$x 50%%"` {
		t.Errorf("systemdQuote = %s", q)
	}
}

func TestRunCommand(t *testing.T) {
	s := spec
	s.Exe = `C:\Users\O'Neil\aka.exe`
	got := RunCommand(s)
	want := `powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -Command "$env:AKA_PROXY_PORT='1355'; $env:AKA_STATE_DIR='/tmp/a&b'; & 'C:\Users\O''Neil\aka.exe' proxy start"`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
