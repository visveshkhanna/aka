package detect

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Listener is a TCP socket in LISTEN state.
type Listener struct {
	PID  int
	IP   string // bound address; "" for a wildcard (0.0.0.0 / ::)
	IPv6 bool
	Port int
}

// DialHost returns the loopback address the proxy should connect to in order
// to reach this listener.
func (l Listener) DialHost() string {
	switch {
	case l.IP != "":
		return l.IP
	case l.IPv6:
		return "::1"
	default:
		return "127.0.0.1"
	}
}

func (l Listener) String() string {
	ip := l.IP
	if ip == "" {
		ip = "*"
	}
	return fmt.Sprintf("%s:%d (pid %d)", ip, l.Port, l.PID)
}

// Listeners returns the LISTEN sockets owned by any of pids. Implemented per
// OS. A process that exits mid-scan is silently skipped.
func Listeners(pids []int) ([]Listener, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	ls, err := listeners(pids)
	sortListeners(ls)
	return dedupe(ls), err
}

func sortListeners(ls []Listener) {
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].Port != ls[j].Port {
			return ls[i].Port < ls[j].Port
		}
		if ls[i].IPv6 != ls[j].IPv6 {
			return !ls[i].IPv6 // prefer IPv4 entries first for a port
		}
		return ls[i].IP < ls[j].IP
	})
}

// dedupe keeps one listener per port (the first after sorting, so IPv4 wins
// when a server binds both families). Forked workers that inherit the socket
// show up under several PIDs; the lowest PID is kept.
func dedupe(ls []Listener) []Listener {
	out := ls[:0]
	seen := map[int]bool{}
	for _, l := range ls {
		if seen[l.Port] {
			continue
		}
		seen[l.Port] = true
		out = append(out, l)
	}
	return out
}

// Ports returns the ports of ls, in order.
func Ports(ls []Listener) []int {
	out := make([]int, len(ls))
	for i, l := range ls {
		out[i] = l.Port
	}
	return out
}

// parseLsof parses `lsof -F ptn` output: one field per line, prefixed by the
// field letter. 'p' starts a process, 'f' starts a file, 't' is the file type
// (IPv4/IPv6) and 'n' the name, e.g. "127.0.0.1:8000", "*:4200", "[::1]:3000".
func parseLsof(out string) []Listener {
	var ls []Listener
	pid := 0
	v6 := false
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		val := line[1:]
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(val)
		case 'f':
			v6 = false
		case 't':
			v6 = val == "IPv6"
		case 'n':
			// Connected sockets look like "a:1->b:2"; LISTEN ones never do,
			// but guard anyway since -sTCP:LISTEN filtering varies.
			if strings.Contains(val, "->") {
				continue
			}
			host, port, ok := splitHostPort(val)
			if !ok {
				continue
			}
			if strings.Contains(host, ":") {
				v6 = true
			}
			ls = append(ls, Listener{PID: pid, IP: normalizeIP(host), IPv6: v6, Port: port})
		}
	}
	return ls
}

func splitHostPort(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	host := strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]")
	return host, port, true
}

// normalizeIP maps wildcard spellings to "" and canonicalises the rest.
func normalizeIP(host string) string {
	switch host {
	case "*", "", "0.0.0.0", "::", "::0":
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return ""
		}
		return ip.String()
	}
	return host
}

// procNetEntry is one LISTEN row from /proc/net/tcp or /proc/net/tcp6.
type procNetEntry struct {
	Inode uint64
	IP    string
	IPv6  bool
	Port  int
}

// tcpListenState is TCP_LISTEN in the kernel's tcp_states.h.
const tcpListenState = "0A"

// parseProcNetTCP parses /proc/net/tcp or /proc/net/tcp6 and returns the
// sockets in LISTEN state.
//
//	sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode
//	 0: 0100007F:1F40 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 12345 ...
func parseProcNetTCP(data string, v6 bool) []procNetEntry {
	var out []procNetEntry
	sc := bufio.NewScanner(strings.NewReader(data))
	first := true
	for sc.Scan() {
		if first { // header
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != tcpListenState {
			continue
		}
		hexIP, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		port, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if err != nil || inode == 0 {
			continue
		}
		ip, ok := decodeProcNetIP(hexIP)
		if !ok {
			continue
		}
		out = append(out, procNetEntry{Inode: inode, IP: normalizeIP(ip), IPv6: v6, Port: int(port)})
	}
	return out
}

// decodeProcNetIP decodes the kernel's hex address: the address is printed as
// 32-bit words in host byte order (little-endian on every platform Go
// supports on Linux except big-endian s390x/ppc64/mips, which we accept as a
// known limitation).
func decodeProcNetIP(h string) (string, bool) {
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return "", false
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	ip := net.IP(b)
	if v4 := ip.To4(); v4 != nil && len(b) == 16 && !ip.IsUnspecified() {
		// IPv4-mapped IPv6 (::ffff:127.0.0.1): report as IPv4.
		return v4.String(), true
	}
	return ip.String(), true
}

// parseSocketInode parses a /proc/<pid>/fd symlink target "socket:[12345]".
func parseSocketInode(link string) (uint64, bool) {
	if !strings.HasPrefix(link, "socket:[") || !strings.HasSuffix(link, "]") {
		return 0, false
	}
	n, err := strconv.ParseUint(link[len("socket:["):len(link)-1], 10, 64)
	return n, err == nil
}
