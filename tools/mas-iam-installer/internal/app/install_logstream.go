package app

import (
	"bytes"
	"io"
	"time"
)

// resumeWriter sits behind `oc logs --timestamps`. It strips the RFC3339Nano
// prefix the kubelet puts on every line and remembers the newest timestamp it
// has printed. After a reconnect with --since-time=<last> (inclusive), the
// replayed lines are skipped. Several lines can share one timestamp (the
// container runtime stamps a chunk read from the pipe once), so it counts the
// lines already shown at <last> and skips exactly that many on replay.
type resumeWriter struct {
	out       io.Writer
	last      time.Time
	lastCount int // lines printed with timestamp == last
	skipAtTs  int // lines still to skip at timestamp == last after a resume
	resuming  bool
	partial   []byte
	written   int64
}

func (w *resumeWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	for {
		idx := bytes.IndexByte(w.partial, '\n')
		if idx < 0 {
			break
		}
		line := w.partial[:idx+1]
		if err := w.emit(line); err != nil {
			return len(p), err
		}
		w.partial = w.partial[idx+1:]
	}
	return len(p), nil
}

// Flush writes a trailing line that had no newline, e.g. the last line before
// the container exited.
func (w *resumeWriter) Flush() error {
	if len(w.partial) == 0 {
		return nil
	}
	line := append(w.partial, '\n')
	w.partial = nil
	return w.emit(line)
}

// Resume prepares for a reattached stream: it discards half a line left by
// the dropped connection (the resumed stream sends it again in full) and arms
// the replay skip.
func (w *resumeWriter) Resume() {
	w.partial = nil
	if !w.last.IsZero() {
		w.resuming = true
		w.skipAtTs = w.lastCount
	}
}

// SinceTime is the --since-time value to resume from, or "" before any line.
func (w *resumeWriter) SinceTime() string {
	if w.last.IsZero() {
		return ""
	}
	return w.last.Format(time.RFC3339Nano)
}

func (w *resumeWriter) emit(line []byte) error {
	body := line
	if sp := bytes.IndexByte(line, ' '); sp > 0 {
		if ts, err := time.Parse(time.RFC3339Nano, string(line[:sp])); err == nil {
			if w.resuming {
				switch {
				case ts.Before(w.last):
					return nil
				case ts.Equal(w.last) && w.skipAtTs > 0:
					w.skipAtTs--
					return nil
				}
				w.resuming = false
			}
			if ts.Equal(w.last) {
				w.lastCount++
			} else {
				w.last = ts
				w.lastCount = 1
			}
			body = line[sp+1:]
		}
	}
	n, err := w.out.Write(body)
	w.written += int64(n)
	return err
}
