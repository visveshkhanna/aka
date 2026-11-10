package detect

import (
	"bytes"
	"regexp"
	"strconv"
	"sync"
)

// Hints collects ports that the child printed as local URLs, such as
// "Local: http://localhost:4200/" or "Uvicorn running on http://127.0.0.1:8000".
// A hint is never trusted alone: the chooser only uses hinted ports that are
// also listening in the child tree.
type Hints struct {
	mu    sync.Mutex
	ports []int // most recently printed last, no duplicates
	seq   int   // bumped on every new hint
}

// Add records a hinted port, moving it to the most-recent position.
func (h *Hints) Add(port int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, p := range h.ports {
		if p == port {
			if i == len(h.ports)-1 {
				return
			}
			h.ports = append(h.ports[:i], h.ports[i+1:]...)
			break
		}
	}
	h.ports = append(h.ports, port)
	h.seq++
}

// Ports returns hinted ports, most recently printed first.
func (h *Hints) Ports() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]int, len(h.ports))
	for i, p := range h.ports {
		out[len(h.ports)-1-i] = p
	}
	return out
}

// Seq returns a counter that changes whenever a hint is added, so pollers can
// react to a fresh hint immediately.
func (h *Hints) Seq() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// ansiRE matches CSI sequences (colours, cursor moves) and OSC sequences
// (titles, OSC 8 hyperlinks) terminated by BEL or ST.
var ansiRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// localURLRE matches a plain-HTTP URL on a loopback or wildcard address with
// an explicit port. HTTPS URLs are ignored: the MVP proxies to plain-HTTP
// upstreams only.
var localURLRE = regexp.MustCompile(`(?i)\bhttp://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1?\]|\[::\]):(\d{1,5})\b`)

// StripANSI removes terminal escape sequences.
func StripANSI(s []byte) []byte { return ansiRE.ReplaceAll(s, nil) }

// ScanLine extracts hinted ports from one line of output.
func ScanLine(line []byte) []int {
	var out []int
	for _, m := range localURLRE.FindAllSubmatch(StripANSI(line), -1) {
		if p, err := strconv.Atoi(string(m[1])); err == nil && p > 0 && p <= 65535 {
			out = append(out, p)
		}
	}
	return out
}

// maxLine bounds the partial-line buffer so a child that never prints a
// newline (progress bars) cannot grow memory without limit.
const maxLine = 8 << 10

// HintWriter is an io.Writer that scans everything written through it for
// local URLs, line by line. Lines end at '\n' or '\r' (progress output often
// redraws with carriage returns).
type HintWriter struct {
	Hints *Hints
	mu    sync.Mutex
	buf   []byte
}

func (w *HintWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	data := p
	for len(data) > 0 {
		i := bytes.IndexAny(data, "\r\n")
		if i < 0 {
			w.buf = append(w.buf, data...)
			if len(w.buf) > maxLine {
				w.flush()
			}
			break
		}
		w.buf = append(w.buf, data[:i]...)
		w.flush()
		data = data[i+1:]
	}
	return len(p), nil
}

// Flush scans any buffered partial line.
func (w *HintWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
}

func (w *HintWriter) flush() {
	if len(w.buf) > 0 {
		for _, port := range ScanLine(w.buf) {
			w.Hints.Add(port)
		}
	}
	w.buf = w.buf[:0]
}
