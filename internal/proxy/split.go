package proxy

import (
	"bufio"
	"errors"
	"net"
	"sync"
	"time"
)

// sniffTimeout is how long a new connection may stay silent before it is
// dropped. Browsers open speculative connections and may send nothing for a
// while, so this is generous. Each connection waits in its own goroutine and
// never holds up others.
const sniffTimeout = 60 * time.Second

// tlsRecordHandshake is the first byte of every TLS connection (a handshake
// record). No HTTP/1.x method or HTTP/2 preface starts with it.
const tlsRecordHandshake = 0x16

// SplitTLS serves HTTP and HTTPS on one port. It accepts connections from ln
// and hands each to plain or tlsRaw by its first byte. tlsRaw yields raw
// connections; serve it with http.Server.ServeTLS. Closing either listener
// closes ln and both halves.
func SplitTLS(ln net.Listener) (plain, tlsRaw net.Listener) {
	sp := &splitter{ln: ln, done: make(chan struct{})}
	sp.plain = &half{sp: sp, ch: make(chan net.Conn)}
	sp.tls = &half{sp: sp, ch: make(chan net.Conn)}
	go sp.acceptLoop()
	return sp.plain, sp.tls
}

type splitter struct {
	ln        net.Listener
	plain     *half
	tls       *half
	done      chan struct{}
	closeOnce sync.Once
}

func (sp *splitter) close() error {
	var err error
	sp.closeOnce.Do(func() {
		err = sp.ln.Close()
		close(sp.done)
	})
	return err
}

func (sp *splitter) acceptLoop() {
	defer sp.close()
	var backoff time.Duration
	for {
		c, err := sp.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// EMFILE and friends: back off and keep serving, as net/http does.
			backoff = min(max(backoff*2, 5*time.Millisecond), time.Second)
			select {
			case <-time.After(backoff):
				continue
			case <-sp.done:
				return
			}
		}
		backoff = 0
		go sp.route(c)
	}
}

func (sp *splitter) route(c net.Conn) {
	c.SetReadDeadline(time.Now().Add(sniffTimeout))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		c.Close()
		return
	}
	c.SetReadDeadline(time.Time{})
	h := sp.plain
	if first[0] == tlsRecordHandshake {
		h = sp.tls
	}
	select {
	case h.ch <- &peekedConn{Conn: c, r: br}:
	case <-sp.done:
		c.Close()
	}
}

// half is one side of a split listener.
type half struct {
	sp *splitter
	ch chan net.Conn
}

func (h *half) Accept() (net.Conn, error) {
	select {
	case c := <-h.ch:
		return c, nil
	case <-h.sp.done:
		return nil, net.ErrClosed
	}
}

func (h *half) Close() error   { return h.sp.close() }
func (h *half) Addr() net.Addr { return h.sp.ln.Addr() }

// peekedConn replays the bytes read while sniffing.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
