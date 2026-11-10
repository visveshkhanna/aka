package trust

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"aka/internal/certs"
)

func TestDetectDistro(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("no") }
	for _, tc := range []struct {
		osRelease string
		want      string
	}{
		{"ID=ubuntu\nID_LIKE=debian\n", "debian"},
		{`ID="rocky"` + "\n" + `ID_LIKE="rhel centos fedora"`, "fedora"},
		{"ID=pop\nID_LIKE=\"ubuntu debian\"", "debian"},
		{"ID=alpine\n", "debian"},
		{"ID=manjaro\nID_LIKE=arch\n", "arch"},
		{"ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"", "suse"},
		{"# comment\nID=garuda\nID_LIKE=arch", "arch"},
		{"ID=Fedora", "fedora"}, // values are case-folded
	} {
		d, ok := detectDistro(tc.osRelease, none)
		if !ok || d.Name != tc.want {
			t.Errorf("%q: got %q %v, want %q", tc.osRelease, d.Name, ok, tc.want)
		}
	}
	// Unknown ID: fall back to whichever update command is installed.
	hasTrust := func(c string) (string, error) {
		if c == "update-ca-trust" {
			return "/usr/bin/" + c, nil
		}
		return "", errors.New("no")
	}
	if d, ok := detectDistro("ID=mystery", hasTrust); !ok || d.Name != "fedora" {
		t.Errorf("fallback: got %q %v", d.Name, ok)
	}
	if _, ok := detectDistro("ID=mystery", none); ok {
		t.Error("detected a distro with nothing to go on")
	}
}

func TestAnchorInstalled(t *testing.T) {
	ca, _, err := certs.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := distro{Name: "test", CertDir: t.TempDir(), Update: "true"}
	if anchorInstalled(ca, d) {
		t.Fatal("installed before copying")
	}
	if !strings.HasSuffix(anchorPath(d), ".crt") {
		t.Fatalf("anchor %s must end in .crt for update-ca-certificates", anchorPath(d))
	}
	os.WriteFile(anchorPath(d), append(ca.CertPEM, '\n'), 0o644)
	if !anchorInstalled(ca, d) {
		t.Fatal("not detected after copying")
	}
	other, _, _ := certs.LoadOrCreateCA(t.TempDir())
	if anchorInstalled(other, d) {
		t.Fatal("a different CA matched the installed anchor")
	}
}

// fakeCommands records commands instead of running them.
func fakeCommands(t *testing.T, root bool, have ...string) *[][]string {
	t.Helper()
	var ran [][]string
	oldRun, oldProbe, oldLook, oldRoot := runner, probe, lookPath, isRoot
	runner = func(name string, args ...string) error {
		ran = append(ran, append([]string{filepath.Base(name)}, args...))
		return nil
	}
	probe = func(name string, args ...string) bool { return true }
	lookPath = func(c string) (string, error) {
		for _, h := range have {
			if c == h {
				return "/usr/bin/" + c, nil
			}
		}
		return "", errors.New("not found")
	}
	isRoot = func() bool { return root }
	t.Cleanup(func() { runner, probe, lookPath, isRoot = oldRun, oldProbe, oldLook, oldRoot })
	return &ran
}

func TestAsRoot(t *testing.T) {
	ran := fakeCommands(t, false, "sudo")
	if err := asRoot(Options{}, "update-ca-certificates"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"sudo", "--", "update-ca-certificates"}}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v, want %v", *ran, want)
	}

	ran = fakeCommands(t, true)
	asRoot(Options{}, "update-ca-certificates")
	if want := [][]string{{"update-ca-certificates"}}; !reflect.DeepEqual(*ran, want) {
		t.Fatalf("as root ran %v, want %v", *ran, want)
	}

	fakeCommands(t, false)
	if err := asRoot(Options{}, "x"); err == nil || !strings.Contains(err.Error(), "sudo is not installed") {
		t.Fatalf("without sudo: %v", err)
	}
}

func TestNSSDiscoveryAndInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, ".mozilla", "firefox", "abc.default")
	legacy := filepath.Join(home, ".mozilla", "firefox", "old.default")
	empty := filepath.Join(home, ".mozilla", "firefox", "empty")
	for _, d := range []string{profile, legacy, empty} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(profile, "cert9.db"), nil, 0o644)
	os.WriteFile(filepath.Join(legacy, "cert8.db"), nil, 0o644)
	want := []string{"sql:" + profile, "dbm:" + legacy} // glob order
	if runtime.GOOS == "linux" {
		chrome := filepath.Join(home, ".pki", "nssdb")
		os.MkdirAll(chrome, 0o755)
		os.WriteFile(filepath.Join(chrome, "cert9.db"), nil, 0o644)
		want = append([]string{"sql:" + chrome}, want...)
	}
	if got := nssDBs(home); !reflect.DeepEqual(got, want) {
		t.Fatalf("nssDBs = %v\nwant %v", got, want)
	}

	ca, _, _ := certs.LoadOrCreateCA(t.TempDir())
	ran := fakeCommands(t, false, "certutil")
	var out strings.Builder
	installNSS(ca, Options{Out: &out})
	if len(*ran) != len(want) {
		t.Fatalf("ran %v", *ran)
	}
	for i, cmd := range *ran {
		if cmd[0] != "certutil" || cmd[1] != "-A" || cmd[3] != want[i] || !strings.Contains(strings.Join(cmd, " "), "-t C,,") {
			t.Errorf("command %v", cmd)
		}
	}

	// No certutil: say how to get it instead of failing.
	fakeCommands(t, false)
	out.Reset()
	installNSS(ca, Options{Out: &out})
	if !strings.Contains(out.String(), "install certutil") {
		t.Fatalf("output %q", out.String())
	}
}

func TestFreshCAIsNotTrusted(t *testing.T) {
	ca, _, err := certs.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if IsTrusted(ca) {
		t.Fatal("a CA that was never installed is reported as trusted")
	}
}
