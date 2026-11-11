//go:build unix

package runner

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/visveshkhanna/aka/internal/brand"
	"github.com/visveshkhanna/aka/internal/detect"
)

// output tees the child's stdout and stderr to ours while scanning them for
// local URLs. When our stream is a terminal the child gets a pseudo-terminal
// for it, so it still sees a TTY (colours, progress output, line buffering).
// Stdin is passed through untouched, and the child stays in our foreground
// process group, so Ctrl+C and interactive prompts behave as if aka were not
// there.
type output struct {
	piped    bool       // at least one stream is a pipe rather than a pty
	parentFD []*os.File // our ends: pty masters or pipe readers
	childFD  []*os.File // child ends, closed in the parent after start
	ptys     []ptyPair
	writers  []*detect.HintWriter
	wg       sync.WaitGroup
	winch    chan os.Signal
}

type ptyPair struct{ master, slave, tty *os.File }

func setupOutput(cmd *exec.Cmd, stdout, stderr *os.File, hints *detect.Hints) (*output, error) {
	o := &output{}
	usePty := os.Getenv(brand.Env("NO_PTY")) == ""
	for i, dst := range []*os.File{stdout, stderr} {
		var parent, child *os.File
		var err error
		if usePty && term.IsTerminal(int(dst.Fd())) {
			parent, child, err = pty.Open()
			if err == nil {
				pty.InheritSize(dst, child)
				rawOutput(child)
				o.ptys = append(o.ptys, ptyPair{parent, child, dst})
			}
		}
		if parent == nil {
			parent, child, err = os.Pipe()
			if err != nil {
				o.abort()
				return nil, err
			}
			if i == 0 {
				o.piped = true
			}
		}
		o.parentFD = append(o.parentFD, parent)
		o.childFD = append(o.childFD, child)
		hw := &detect.HintWriter{Hints: hints}
		o.writers = append(o.writers, hw)
		if i == 0 {
			cmd.Stdout = child
		} else {
			cmd.Stderr = child
		}
		o.wg.Add(1)
		go func(src io.Reader, dst io.Writer) {
			defer o.wg.Done()
			io.Copy(dst, src) // ends with EOF (pipe) or EIO (pty) when the child side closes
		}(parent, io.MultiWriter(dst, hw))
	}
	return o, nil
}

// started closes the child's ends in the parent (so EOF arrives when the
// child exits) and starts forwarding terminal resizes.
func (o *output) started() {
	for _, f := range o.childFD {
		f.Close()
	}
	if len(o.ptys) > 0 {
		o.winch = make(chan os.Signal, 1)
		signal.Notify(o.winch, syscall.SIGWINCH)
		go func() {
			for range o.winch {
				for _, p := range o.ptys {
					pty.InheritSize(p.tty, p.master)
				}
			}
		}()
	}
}

func (o *output) abort() {
	for _, f := range o.childFD {
		f.Close()
	}
	for _, f := range o.parentFD {
		f.Close()
	}
}

// finish waits briefly for buffered output to drain, then closes our ends.
// A leftover process holding the child end open must not block exit.
func (o *output) finish() {
	if o.winch != nil {
		signal.Stop(o.winch)
		close(o.winch)
	}
	done := make(chan struct{})
	go func() { o.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}
	for _, f := range o.parentFD {
		f.Close()
	}
	for _, w := range o.writers {
		w.Flush()
	}
}
