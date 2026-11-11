package detect

import (
	"encoding/binary"
	"net"
	"reflect"
	"testing"
)

func tcpRow(state uint32, ip net.IP, port uint16, pid uint32) []byte {
	r := make([]byte, tcpRowSize)
	binary.LittleEndian.PutUint32(r[0:4], state)
	copy(r[4:8], ip.To4())
	binary.BigEndian.PutUint16(r[8:10], port) // network order in the low word
	binary.LittleEndian.PutUint32(r[20:24], pid)
	return r
}

func tcp6Row(state uint32, ip net.IP, port uint16, pid uint32) []byte {
	r := make([]byte, tcp6RowSize)
	copy(r[0:16], ip.To16())
	binary.BigEndian.PutUint16(r[20:22], port)
	binary.LittleEndian.PutUint32(r[48:52], state)
	binary.LittleEndian.PutUint32(r[52:56], pid)
	return r
}

func table(rows ...[]byte) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(len(rows)))
	for _, r := range rows {
		b = append(b, r...)
	}
	return b
}

func TestParseTCPTable(t *testing.T) {
	b := table(
		tcpRow(mibTCPListen, net.IPv4(127, 0, 0, 1), 4123, 900),
		tcpRow(mibTCPListen, net.IPv4zero, 8000, 901),
		tcpRow(5, net.IPv4(127, 0, 0, 1), 50000, 900), // ESTABLISHED: skipped
	)
	got, err := parseTCPTable(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Listener{{PID: 900, IP: "127.0.0.1", Port: 4123}, {PID: 901, IP: "", Port: 8000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseTCP6Table(t *testing.T) {
	b := table(
		tcp6Row(mibTCPListen, net.IPv6loopback, 4200, 77),
		tcp6Row(mibTCPListen, net.IPv6unspecified, 443, 78),
	)
	got, err := parseTCP6Table(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []Listener{{PID: 77, IP: "::1", IPv6: true, Port: 4200}, {PID: 78, IPv6: true, Port: 443}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseTCPTableTruncated(t *testing.T) {
	b := table(tcpRow(mibTCPListen, net.IPv4(127, 0, 0, 1), 1, 1))
	for _, bad := range [][]byte{nil, b[:3], b[:len(b)-1]} {
		if _, err := parseTCPTable(bad); err == nil {
			t.Errorf("accepted %d-byte table", len(bad))
		}
	}
	empty := table()
	if ls, err := parseTCP6Table(empty); err != nil || len(ls) != 0 {
		t.Errorf("empty table: %v %v", ls, err)
	}
}

func TestFilterPIDs(t *testing.T) {
	ls := []Listener{{PID: 1, Port: 1}, {PID: 2, Port: 2}, {PID: 3, Port: 3}}
	if got := filterPIDs(ls, []int{3, 1}); len(got) != 2 || got[0].PID != 1 || got[1].PID != 3 {
		t.Fatalf("got %+v", got)
	}
}
