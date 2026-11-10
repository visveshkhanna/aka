//go:build !unix

package runner

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"aka/internal/detect"
)

// Windows: plain pipes until ConPTY support lands with the Windows step.
type output struct {
	piped    bool
	parentFD []*os.File
	childFD  []*os.File
	writers  []*detect.HintWriter
	wg       sync.WaitGroup
}

func setupOutput(cmd *exec.Cmd, stdout, stderr *os.File, hints *detect.Hints) (*output, error) {
	o := &output{piped: true}
	for i, dst := range []*os.File{stdout, stderr} {
		r, w, err := os.Pipe()
		if err != nil {
			o.abort()
			return nil, err
		}
		o.parentFD, o.childFD = append(o.parentFD, r), append(o.childFD, w)
		hw := &detect.HintWriter{Hints: hints}
		o.writers = append(o.writers, hw)
		if i == 0 {
			cmd.Stdout = w
		} else {
			cmd.Stderr = w
		}
		o.wg.Add(1)
		go func(src io.Reader, dst io.Writer) { defer o.wg.Done(); io.Copy(dst, src) }(r, io.MultiWriter(dst, hw))
	}
	return o, nil
}

func (o *output) started() {
	for _, f := range o.childFD {
		f.Close()
	}
}

func (o *output) abort() {
	for _, f := range append(o.childFD, o.parentFD...) {
		f.Close()
	}
}

func (o *output) finish() {
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
