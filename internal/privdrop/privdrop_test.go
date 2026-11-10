//go:build unix

package privdrop

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"syscall"
	"testing"
)

// The drop is irreversible, so it runs in a child process: the test binary
// re-executed with PRIVDROP_CHILD set.
func TestMain(m *testing.M) {
	if os.Getenv("PRIVDROP_CHILD") == "1" {
		os.Exit(child())
	}
	os.Exit(m.Run())
}

func child() int {
	u, ok := SudoUser()
	if !ok {
		fmt.Println("SudoUser: not found")
		return 1
	}
	if err := To(u); err != nil {
		fmt.Println("To:", err)
		return 1
	}
	fmt.Printf("uid=%d euid=%d gid=%d home=%s sudo_uid=%q", os.Getuid(), os.Geteuid(), os.Getgid(), os.Getenv("HOME"), os.Getenv("SUDO_UID"))
	if syscall.Setuid(0) == nil {
		fmt.Print(" REGAINED")
	}
	return 0
}

func TestSudoUserRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	t.Setenv("SUDO_UID", "1")
	if _, ok := SudoUser(); ok {
		t.Fatal("SudoUser returned a user while not root")
	}
}

func TestDropToSudoUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root (runs in the Linux container test)")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("no nobody user")
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "PRIVDROP_CHILD=1", "SUDO_UID="+nobody.Uid, "SUDO_USER=nobody")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := fmt.Sprintf("uid=%s euid=%s gid=%s home=%s sudo_uid=\"\"", nobody.Uid, nobody.Uid, nobody.Gid, nobody.HomeDir)
	if string(out) != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestRootWithoutSudoHasNobodyToDropTo(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	t.Setenv("SUDO_UID", "")
	if _, ok := SudoUser(); ok {
		t.Fatal("SudoUser found a user without SUDO_UID")
	}
}
