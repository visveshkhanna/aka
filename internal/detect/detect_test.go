package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTree(t *testing.T) {
	procs := []Proc{
		{PID: 1, PPID: 0, PGID: 1},
		{PID: 100, PPID: 1, PGID: 100},   // our CLI
		{PID: 200, PPID: 100, PGID: 100}, // sh -c
		{PID: 300, PPID: 200, PGID: 100}, // npm
		{PID: 400, PPID: 300, PGID: 100}, // node (the real server, a great-grandchild)
		{PID: 500, PPID: 1, PGID: 100},   // daemonised: reparented to init, still in our pgid
		{PID: 600, PPID: 1, PGID: 600},   // unrelated
		{PID: 700, PPID: 600, PGID: 600}, // unrelated child
		{PID: 800, PPID: 400, PGID: 800}, // descendant that called setsid: still found via parent chain
	}
	cases := []struct {
		name                string
		root, pgid, exclude int
		want                []int
	}{
		{"descendants only", 200, 0, 0, []int{200, 300, 400, 800}},
		{"with process group", 200, 100, 100, []int{200, 300, 400, 500, 800}},
		{"exclude self", 100, 100, 100, []int{200, 300, 400, 500, 800}},
		{"unrelated tree", 600, 0, 0, []int{600, 700}},
		{"root missing from snapshot", 999, 0, 0, []int{999}},
		{"nothing", 0, 0, 0, []int{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Tree(procs, c.root, c.pgid, c.exclude)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Tree = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTreeSurvivesCycles(t *testing.T) {
	// A corrupt or racing snapshot must not hang the walker.
	procs := []Proc{{PID: 10, PPID: 20, PGID: 0}, {PID: 20, PPID: 10, PGID: 0}, {PID: 30, PPID: 30, PGID: 0}}
	got := Tree(procs, 10, 0, 0)
	if !reflect.DeepEqual(got, []int{10, 20}) {
		t.Fatalf("got %v", got)
	}
}

func TestListProcsIncludesSelf(t *testing.T) {
	procs, err := ListProcs()
	if err != nil {
		t.Skipf("process listing unsupported: %v", err)
	}
	me := os.Getpid()
	for _, p := range procs {
		if p.PID == me {
			if p.PPID != os.Getppid() {
				t.Fatalf("ppid = %d, want %d", p.PPID, os.Getppid())
			}
			return
		}
	}
	t.Fatalf("own pid %d not in process list of %d entries", me, len(procs))
}

func TestParseProcStat(t *testing.T) {
	cases := []struct {
		line string
		want Proc
		ok   bool
	}{
		{"1234 (python3) S 1 1234 1234 0 -1 4194560", Proc{PID: 1234, PPID: 1, PGID: 1234}, true},
		{"42 (my (weird) cmd) R 7 9 9 0", Proc{PID: 42, PPID: 7, PGID: 9}, true},
		{"5 (sp ace) S 2 3 3", Proc{PID: 5, PPID: 2, PGID: 3}, true},
		{"garbage", Proc{}, false},
		{"5 (x) S", Proc{}, false},
		{"5 (x) S a b", Proc{}, false},
	}
	for _, c := range cases {
		pid := 0
		fmt.Sscanf(c.line, "%d", &pid)
		got, ok := parseProcStat(pid, c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("parseProcStat(%q) = %+v,%v want %+v,%v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestParseLsof(t *testing.T) {
	out := strings.Join([]string{
		"p4001",
		"f12",
		"tIPv4",
		"n127.0.0.1:8000",
		"f13",
		"tIPv6",
		"n[::1]:8000",
		"p4002",
		"f20",
		"tIPv4",
		"n*:4200",
		"f21",
		"tIPv6",
		"n*:9229",
		"f22",
		"tIPv4",
		"n127.0.0.1:5000->127.0.0.1:61000", // connected, ignored
		"f23",
		"tIPv4",
		"nnot-a-socket",
		"",
	}, "\n")
	got := parseLsof(out)
	want := []Listener{
		{PID: 4001, IP: "127.0.0.1", Port: 8000},
		{PID: 4001, IP: "::1", IPv6: true, Port: 8000},
		{PID: 4002, IP: "", Port: 4200},
		{PID: 4002, IP: "", IPv6: true, Port: 9229},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLsof:\n got %+v\nwant %+v", got, want)
	}
}

func TestDialHost(t *testing.T) {
	cases := []struct {
		l    Listener
		want string
	}{
		{Listener{IP: "127.0.0.1"}, "127.0.0.1"},
		{Listener{IP: "::1", IPv6: true}, "::1"},
		{Listener{}, "127.0.0.1"},
		{Listener{IPv6: true}, "::1"},
	}
	for _, c := range cases {
		if got := c.l.DialHost(); got != c.want {
			t.Errorf("%+v.DialHost() = %q, want %q", c.l, got, c.want)
		}
	}
}

func TestDedupePrefersIPv4AndLowestPort(t *testing.T) {
	ls := []Listener{
		{PID: 2, IP: "::1", IPv6: true, Port: 8000},
		{PID: 1, IP: "127.0.0.1", Port: 8000},
		{PID: 3, Port: 3000},
	}
	sortListeners(ls)
	got := dedupe(ls)
	want := []Listener{{PID: 3, Port: 3000}, {PID: 1, IP: "127.0.0.1", Port: 8000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

const procNetTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F40 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 11111 1 0000000000000000 100 0 0 10 0
   1: 00000000:1068 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1F40 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 33333 1 0000000000000000 20 4 30 10 -1
   3: 0100007F:1F41 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 0 1 0000000000000000 100 0 0 10 0
`

const procNetTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 44444 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:1389 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 55555 1 0000000000000000 100 0 0 10 0
   2: 0000000000000000FFFF00000100007F:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 66666 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCP(t *testing.T) {
	got := parseProcNetTCP(procNetTCP, false)
	want := []procNetEntry{
		{Inode: 11111, IP: "127.0.0.1", Port: 8000},
		{Inode: 22222, IP: "", Port: 4200},
		// row 2 is ESTABLISHED, row 3 has inode 0: both skipped
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tcp:\n got %+v\nwant %+v", got, want)
	}
	got6 := parseProcNetTCP(procNetTCP6, true)
	want6 := []procNetEntry{
		{Inode: 44444, IP: "::1", IPv6: true, Port: 3000},
		{Inode: 55555, IP: "", IPv6: true, Port: 5001},
		{Inode: 66666, IP: "127.0.0.1", IPv6: true, Port: 8080}, // v4-mapped
	}
	if !reflect.DeepEqual(got6, want6) {
		t.Fatalf("tcp6:\n got %+v\nwant %+v", got6, want6)
	}
}

func TestParseSocketInode(t *testing.T) {
	if n, ok := parseSocketInode("socket:[12345]"); !ok || n != 12345 {
		t.Fatalf("got %d %v", n, ok)
	}
	for _, bad := range []string{"pipe:[1]", "/dev/null", "socket:[x]", "socket:[1"} {
		if _, ok := parseSocketInode(bad); ok {
			t.Errorf("parseSocketInode(%q) accepted", bad)
		}
	}
}

// TestFakeProcfs runs the Linux readers against a synthetic /proc so the
// fd-to-inode join is covered on every OS.
func TestFakeProcfs(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "net"), 0o755))
	must(os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(procNetTCP), 0o644))
	must(os.WriteFile(filepath.Join(root, "net", "tcp6"), []byte(procNetTCP6), 0o644))
	mkproc := func(pid, ppid, pgid int, links ...string) {
		dir := filepath.Join(root, fmt.Sprint(pid))
		must(os.MkdirAll(filepath.Join(dir, "fd"), 0o755))
		must(os.WriteFile(filepath.Join(dir, "stat"),
			[]byte(fmt.Sprintf("%d (proc %d) S %d %d %d 0", pid, pid, ppid, pgid, pgid)), 0o644))
		for i, l := range links {
			must(os.Symlink(l, filepath.Join(dir, "fd", fmt.Sprint(i))))
		}
	}
	mkproc(10, 1, 10, "/dev/null", "socket:[11111]", "pipe:[9]")
	mkproc(11, 10, 10, "socket:[44444]", "socket:[33333]") // 33333 is not LISTEN
	mkproc(12, 1, 12, "socket:[22222]")                    // not ours

	procs, err := listProcsFrom(root)
	must(err)
	tree := Tree(procs, 10, 0, 0)
	if !reflect.DeepEqual(tree, []int{10, 11}) {
		t.Fatalf("tree = %v", tree)
	}
	ls, err := listenersFrom(root, tree)
	must(err)
	sortListeners(ls)
	want := []Listener{
		{PID: 11, IP: "::1", IPv6: true, Port: 3000},
		{PID: 10, IP: "127.0.0.1", Port: 8000},
	}
	if !reflect.DeepEqual(ls, want) {
		t.Fatalf("listeners:\n got %+v\nwant %+v", ls, want)
	}
}

func TestChoose(t *testing.T) {
	l := func(port int) Listener { return Listener{PID: 1, IP: "127.0.0.1", Port: port} }
	httpPorts := map[int]bool{8000: true, 8001: true, 4200: true}
	probe := func(x Listener) bool { return httpPorts[x.Port] }

	cases := []struct {
		name   string
		in     Input
		port   int
		reason Reason
		ok     bool
	}{
		{"nothing listening", Input{Assigned: 4100, Probe: probe}, 0, "", false},
		{"assigned wins over everything",
			Input{Assigned: 4100, Listeners: []Listener{l(4100), l(8000)}, Hints: []int{8000}, Current: 8000, Probe: probe},
			4100, ReasonAssigned, true},
		{"printed URL when PORT ignored",
			Input{Assigned: 4100, Listeners: []Listener{l(4200), l(8001)}, Hints: []int{4200}, Probe: probe},
			4200, ReasonPrinted, true},
		{"most recent printed URL that listens",
			Input{Listeners: []Listener{l(8000), l(8001)}, Hints: []int{9999, 8001, 8000}, Probe: probe},
			8001, ReasonPrinted, true},
		{"printed URL not listening is ignored",
			Input{Listeners: []Listener{l(8000)}, Hints: []int{3000}, Probe: probe},
			8000, ReasonProbed, true},
		{"keeps current port to avoid flapping",
			Input{Listeners: []Listener{l(8000), l(8001)}, Current: 8001, Probe: probe},
			8001, ReasonCurrent, true},
		{"current gone falls through to probe",
			Input{Listeners: []Listener{l(8000)}, Current: 8001, Probe: probe},
			8000, ReasonProbed, true},
		{"probe skips debugger and non-HTTP ports",
			Input{Listeners: []Listener{l(5678), l(7000), l(8000), l(9229)}, Probe: probe},
			8000, ReasonProbed, true},
		{"no HTTP answer means no choice yet",
			Input{Listeners: []Listener{l(7000), l(9229)}, Probe: probe},
			0, "", false},
		{"nil probe skips probe tier",
			Input{Listeners: []Listener{l(8000)}},
			0, "", false},
		{"debugger port still chosen when printed",
			Input{Listeners: []Listener{l(9229)}, Hints: []int{9229}, Probe: probe},
			9229, ReasonPrinted, true},
		{"pinned port overrides assigned",
			Input{Pinned: 8001, Assigned: 4100, Listeners: []Listener{l(4100), l(8001)}, Probe: probe},
			8001, ReasonPinned, true},
		{"pinned port not yet listening waits",
			Input{Pinned: 8001, Assigned: 4100, Listeners: []Listener{l(4100)}, Probe: probe},
			0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Choose(c.in)
			if ok != c.ok || got.Listener.Port != c.port || got.Reason != c.reason {
				t.Fatalf("Choose = %+v,%v want port %d reason %q ok %v", got, ok, c.port, c.reason, c.ok)
			}
		})
	}
}

func TestProbeCache(t *testing.T) {
	calls := map[int]int{}
	answer := map[int]bool{1: true, 2: false}
	c := &ProbeCache{TTL: time.Hour, Probe: func(l Listener) bool { calls[l.Port]++; return answer[l.Port] }}
	for i := 0; i < 3; i++ {
		c.Check(Listener{Port: 1})
		c.Check(Listener{Port: 2})
	}
	if calls[1] != 1 || calls[2] != 1 {
		t.Fatalf("calls = %v, want one probe each", calls)
	}
	c.Retain([]Listener{{Port: 1}}) // port 2 stopped listening
	answer[2] = true
	if !c.Check(Listener{Port: 2}) || calls[2] != 2 {
		t.Fatalf("port 2 should be re-probed after it disappeared; calls=%v", calls)
	}
}

func TestScanLine(t *testing.T) {
	cases := []struct {
		line string
		want []int
	}{
		{"Starting development server at http://127.0.0.1:8000/", []int{8000}},
		{"INFO:     Uvicorn running on http://127.0.0.1:8000 (Press CTRL+C to quit)", []int{8000}},
		{"  ➜  Local:   http://localhost:5173/", []int{5173}},
		{"** Angular Live Development Server is listening on localhost:4200, open your browser on http://localhost:4200/ **", []int{4200}},
		{"  Local URL: http://localhost:8501", []int{8501}},
		{"[2024] Listening at: http://0.0.0.0:8000 (123)", []int{8000}},
		{"* Listening on http://[::1]:3000", []int{3000}},
		{"* Listening on http://[::]:3001", []int{3001}},
		{"Network: http://192.168.1.4:4200/", nil},
		{"Serving HTTPS on https://localhost:8443", nil},
		{"proxy at http://myapp.localhost:1355", nil},
		{"http://localhost:99999 is not a port", nil},
		{"two: http://localhost:1/ and HTTP://LOCALHOST:2", []int{1, 2}},
		// Vite colours the port separately.
		{"  \x1b[32m➜\x1b[39m  \x1b[1mLocal\x1b[22m:   \x1b[36mhttp://localhost:\x1b[1m5173\x1b[22m/\x1b[39m", []int{5173}},
		// OSC 8 hyperlink wrapping the URL.
		{"\x1b]8;;http://localhost:3000\x1b\\click\x1b]8;;\x1b\\", nil},
	}
	for _, c := range cases {
		if got := ScanLine([]byte(c.line)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ScanLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestHintWriter(t *testing.T) {
	h := &Hints{}
	w := &HintWriter{Hints: h}
	// URL split across writes, CR-terminated progress lines, and a final
	// line without a newline.
	for _, chunk := range []string{"Local: http://loc", "alhost:4200/\n", "building 10%\rbuilding 100%\r", "ready on http://127.0.0.1:8000"} {
		w.Write([]byte(chunk))
	}
	if got := h.Ports(); !reflect.DeepEqual(got, []int{4200}) {
		t.Fatalf("before flush: %v", got)
	}
	w.Flush()
	if got := h.Ports(); !reflect.DeepEqual(got, []int{8000, 4200}) {
		t.Fatalf("after flush: %v", got)
	}
	seq := h.Seq()
	w.Write([]byte("again http://localhost:4200\n"))
	if got := h.Ports(); !reflect.DeepEqual(got, []int{4200, 8000}) {
		t.Fatalf("reprinted port should move to front: %v", got)
	}
	if h.Seq() == seq {
		t.Fatal("Seq should change when order changes")
	}
	seq = h.Seq()
	w.Write([]byte("again http://localhost:4200\n"))
	if h.Seq() != seq {
		t.Fatal("Seq should not change when the newest hint repeats")
	}
}

func TestHintWriterBoundsBuffer(t *testing.T) {
	w := &HintWriter{Hints: &Hints{}}
	w.Write([]byte(strings.Repeat("x", 3*maxLine)))
	if len(w.buf) > maxLine {
		t.Fatalf("buffer grew to %d", len(w.buf))
	}
}
