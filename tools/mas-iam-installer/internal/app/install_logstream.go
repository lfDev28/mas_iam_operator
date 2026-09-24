package app

import (
	"bytes"
	"io"
	"time"
)

// resumeWriter sits behind `oc logs --timestamps`. It strips the RFC3339Nano
// prefix the kubelet puts on every line, remembers the newest timestamp it has
// printed, and drops any line at or before it. That lets a dropped follow be
// reattached with --since-time=<last> without replaying the log: --since-time
// is inclusive, so the boundary line would otherwise print twice.
type resumeWriter struct {
	out     io.Writer
	last    time.Time
	partial []byte
	written int64
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

// ResetPartial discards half a line left by a dropped connection; the resumed
// stream sends that line again in full.
func (w *resumeWriter) ResetPartial() {
	w.partial = nil
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
			if !w.last.IsZero() && !ts.After(w.last) {
				return nil
			}
			w.last = ts
			body = line[sp+1:]
		}
	}
	n, err := w.out.Write(body)
	w.written += int64(n)
	return err
}
