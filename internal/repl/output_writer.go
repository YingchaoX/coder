package repl

import (
	"io"
	"sync"
)

// terminalOutputWriter normalizes bare '\n' to "\r\n" for TTY runtime output
// while preserving already-correct "\r\n" sequences.
type terminalOutputWriter struct {
	out io.Writer

	mu     sync.Mutex
	lastCR bool
}

func newTerminalOutputWriter(out io.Writer) io.Writer {
	return &terminalOutputWriter{out: out}
}

func (w *terminalOutputWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	converted := make([]byte, 0, len(p)+8)
	prevCR := w.lastCR
	for _, b := range p {
		if b == '\n' && !prevCR {
			converted = append(converted, '\r', '\n')
			prevCR = false
			continue
		}
		converted = append(converted, b)
		prevCR = b == '\r'
	}
	w.lastCR = prevCR
	if _, err := w.out.Write(converted); err != nil {
		return 0, err
	}
	return len(p), nil
}
