package detect

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	getExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER

// listeners reads the IPv4 and IPv6 LISTEN tables with their owning PIDs
// (GetExtendedTcpTable; no privileges needed) and keeps those owned by
// pids.
func listeners(pids []int) ([]Listener, error) {
	var all []Listener
	for _, af := range []struct {
		family uint32
		parse  func([]byte) ([]Listener, error)
	}{{windows.AF_INET, parseTCPTable}, {windows.AF_INET6, parseTCP6Table}} {
		b, err := tcpTable(af.family)
		if err != nil {
			if af.family == windows.AF_INET6 {
				continue // no IPv6 stack
			}
			return nil, err
		}
		ls, err := af.parse(b)
		if err != nil {
			return nil, err
		}
		all = append(all, ls...)
	}
	return filterPIDs(all, pids), nil
}

func tcpTable(family uint32) ([]byte, error) {
	size := uint32(16 << 10)
	for range 5 { // the table can grow between the size query and the read
		buf := make([]byte, size)
		r, _, _ := getExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
			0, uintptr(family), tcpTableOwnerPIDListener, 0)
		switch windows.Errno(r) {
		case 0:
			return buf[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			size += 4 << 10
			continue
		default:
			return nil, windows.Errno(r)
		}
	}
	return nil, errors.New("GetExtendedTcpTable: table kept growing")
}
