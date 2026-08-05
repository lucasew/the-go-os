// Package logemit formats unit log lines to init stderr (SPEC §8).
package logemit

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Emitter writes attributed journal-style lines.
type Emitter struct {
	mu  sync.Mutex
	out io.Writer
}

// New returns an emitter writing to out (typically os.Stderr of PID 1).
func New(out io.Writer) *Emitter {
	return &Emitter{out: out}
}

// Line emits one payload line for a unit stream.
// Format: time unit=NAME stream=STREAM: payload
func (e *Emitter) Line(unit, stream, payload string) {
	if e == nil || e.out == nil {
		return
	}
	payload = strings.TrimRight(payload, "\r\n")
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintf(e.out, "%s unit=%s stream=%s: %s\n", ts, unit, stream, payload)
}

// System emits a supervisor/init message (unit=init).
func (e *Emitter) System(payload string) {
	e.Line("init", "stderr", payload)
}
