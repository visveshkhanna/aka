package trust

import (
	"reflect"
	"strings"
	"testing"

	"github.com/visveshkhanna/aka/internal/certs"
)

func TestLinuxInstallCommands(t *testing.T) {
	ca, _, err := certs.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, ok := detectDistro(readOSRelease(), lookPath)
	if !ok {
		t.Skip("unknown distro")
	}
	t.Setenv("HOME", t.TempDir()) // no NSS databases
	ran := fakeCommands(t, false, "sudo", d.Update)
	if err := Install(ca, Options{}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"sudo", "--", "mkdir", "-p", d.CertDir},
		{"sudo", "--", "install", "-m", "0644", certs.CertPath(ca.Dir), anchorPath(d)},
		{"sudo", "--", d.Update},
	}
	if !reflect.DeepEqual(*ran, want) {
		t.Fatalf("ran %v\nwant %v", *ran, want)
	}
}

func TestLinuxInstallWithoutUpdateToolChangesNothing(t *testing.T) {
	ca, _, err := certs.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := detectDistro(readOSRelease(), lookPath); !ok {
		t.Skip("unknown distro")
	}
	ran := fakeCommands(t, false, "sudo")
	err = Install(ca, Options{})
	if err == nil || !strings.Contains(err.Error(), "ca-certificates package") {
		t.Fatalf("err = %v", err)
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %v before failing", *ran)
	}
}
