package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTryLockExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	// flock locks are per open file description, so a second open in the
	// same process contends like another process would.
	if _, err := TryLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second TryLock: got %v, want ErrLocked", err)
	}
	unlock()
	unlock2, err := TryLock(p)
	if err != nil {
		t.Fatalf("TryLock after unlock: %v", err)
	}
	unlock2()
}

func TestLockWaits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		u, err := Lock(p)
		if err != nil {
			t.Error(err)
		} else {
			u()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("Lock returned while the lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("Lock did not return after unlock")
	}
}
