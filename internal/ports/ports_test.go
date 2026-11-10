package ports

import (
	"net"
	"testing"
)

func TestBlockedPortsIncludeBrowserBadPorts(t *testing.T) {
	for _, p := range []int{0, 22, 6000, 6667, 10080, 4045, 4190} {
		if !Blocked[p] {
			t.Errorf("port %d should be blocked", p)
		}
	}
	for _, p := range []int{3000, 4000, 4200, 8000} {
		if Blocked[p] {
			t.Errorf("port %d should not be blocked", p)
		}
	}
}

func TestIsFreeDetectsLoopbackOnlyBind(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if IsFree(ln.Addr().(*net.TCPAddr).Port) {
		t.Fatal("port bound on 127.0.0.1 reported free")
	}
}

func TestIsFreeDetectsIPv6LoopbackBind(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback")
	}
	defer ln.Close()
	if IsFree(ln.Addr().(*net.TCPAddr).Port) {
		t.Fatal("port bound on ::1 reported free")
	}
}

func TestPickSkipsTakenAndReserved(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	taken := ln.Addr().(*net.TCPAddr).Port
	// A range of three ports: one bound, one reserved by skip, one free.
	reserved := taken + 1
	p, err := Pick(taken, taken+2, func(p int) bool { return p == reserved })
	if err != nil {
		t.Skipf("neighbouring ports busy: %v", err)
	}
	if p != taken+2 {
		t.Fatalf("Pick = %d, want %d", p, taken+2)
	}
}

func TestPickRejectsInvalidRangeAndExhaustion(t *testing.T) {
	if _, err := Pick(10, 5, nil); err == nil {
		t.Fatal("expected error for min > max")
	}
	if _, err := Pick(6000, 6000, nil); err == nil { // the only candidate is blocked
		t.Fatal("expected error when every port is blocked")
	}
}

func TestPickDefaultRange(t *testing.T) {
	p, err := Pick(MinAppPort, MaxAppPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p < MinAppPort || p > MaxAppPort || Blocked[p] {
		t.Fatalf("Pick returned %d", p)
	}
}
