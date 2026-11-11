package detect

import (
	"encoding/binary"
	"errors"
	"net"
)

// Windows GetExtendedTcpTable layouts (TCP_TABLE_OWNER_PID_LISTENER). Kept
// free of build tags so the parsers are tested on every platform.
//
//	MIB_TCPTABLE_OWNER_PID:  DWORD dwNumEntries; MIB_TCPROW_OWNER_PID table[]
//	MIB_TCPROW_OWNER_PID:    dwState, dwLocalAddr, dwLocalPort, dwRemoteAddr,
//	                         dwRemotePort, dwOwningPid (6 DWORDs, 24 bytes)
//	MIB_TCP6ROW_OWNER_PID:   ucLocalAddr[16], dwLocalScopeId, dwLocalPort,
//	                         ucRemoteAddr[16], dwRemoteScopeId, dwRemotePort,
//	                         dwState, dwOwningPid (56 bytes)
//
// DWORDs are little-endian. Addresses and the low 16 bits of the port DWORDs
// are in network byte order.
const (
	tcpRowSize    = 24
	tcp6RowSize   = 56
	mibTCPListen  = 2
	tcpTableStart = 4
)

var errShortTable = errors.New("truncated TCP table")

// parseTCPTable decodes an IPv4 owner-PID table into listeners.
func parseTCPTable(b []byte) ([]Listener, error) {
	rows, err := tableRows(b, tcpRowSize)
	if err != nil {
		return nil, err
	}
	var out []Listener
	for _, r := range rows {
		if binary.LittleEndian.Uint32(r[0:4]) != mibTCPListen {
			continue
		}
		ip := net.IPv4(r[4], r[5], r[6], r[7])
		out = append(out, Listener{
			PID:  int(binary.LittleEndian.Uint32(r[20:24])),
			IP:   boundIP(ip),
			Port: int(r[8])<<8 | int(r[9]),
		})
	}
	return out, nil
}

// parseTCP6Table decodes an IPv6 owner-PID table into listeners.
func parseTCP6Table(b []byte) ([]Listener, error) {
	rows, err := tableRows(b, tcp6RowSize)
	if err != nil {
		return nil, err
	}
	var out []Listener
	for _, r := range rows {
		if binary.LittleEndian.Uint32(r[48:52]) != mibTCPListen {
			continue
		}
		ip := net.IP(append([]byte{}, r[0:16]...))
		out = append(out, Listener{
			PID:  int(binary.LittleEndian.Uint32(r[52:56])),
			IP:   boundIP(ip),
			IPv6: true,
			Port: int(r[20])<<8 | int(r[21]),
		})
	}
	return out, nil
}

func tableRows(b []byte, size int) ([][]byte, error) {
	if len(b) < tcpTableStart {
		return nil, errShortTable
	}
	n := int(binary.LittleEndian.Uint32(b[0:4]))
	if n < 0 || tcpTableStart+n*size > len(b) {
		return nil, errShortTable
	}
	rows := make([][]byte, n)
	for i := range rows {
		off := tcpTableStart + i*size
		rows[i] = b[off : off+size]
	}
	return rows, nil
}

// boundIP is "" for a wildcard address, matching the other platforms.
func boundIP(ip net.IP) string {
	if ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// filterPIDs keeps listeners owned by one of pids.
func filterPIDs(ls []Listener, pids []int) []Listener {
	want := make(map[int]bool, len(pids))
	for _, p := range pids {
		want[p] = true
	}
	var out []Listener
	for _, l := range ls {
		if want[l.PID] {
			out = append(out, l)
		}
	}
	return out
}
