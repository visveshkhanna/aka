package netx

import (
	"net"
	"testing"
)

func TestRealErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if _, err := net.Listen("tcp", addr); !IsAddrInUse(err) {
		t.Errorf("second bind: %v not classified as in use", err)
	}
	ln.Close()
	_, err = net.Dial("tcp", addr)
	if !IsRefused(err) {
		t.Errorf("dial closed port: %v not classified as refused", err)
	}
	if IsRefused(nil) || IsAddrInUse(nil) || IsPermission(nil) {
		t.Error("nil classified as an error")
	}
}
