package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Spinner renders a single-line animated frame on stdout to signal that a
// long-running phase is in progress. Stop() replaces the line with the final
// status marker so the spinner doesn't leave artefacts behind.
//
// Non-interactive runs (no TTY) skip the animation and only print the start
// line, so spinner output doesn't pollute log captures or CI buffers.
type Spinner struct {
	label string
	out   io.Writer
	mu    sync.Mutex
	stop  chan struct{}
	done  chan struct{}
	live  bool
}

var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

func NewSpinner(label string) *Spinner {
	return &Spinner{
		label: label,
		out:   os.Stdout,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// Start kicks off the animation goroutine on interactive TTYs. On a
// non-TTY output it just prints a single "▶ label" line and returns.
func (s *Spinner) Start() {
	if !IsInteractive() {
		fmt.Fprintf(s.out, "▶ %s\n", s.label)
		close(s.done)
		return
	}
	s.live = true
	start := time.Now()
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				s.mu.Lock()
				fmt.Fprint(s.out, "\r\033[K")
				s.mu.Unlock()
				return
			case <-ticker.C:
				s.mu.Lock()
				fmt.Fprintf(s.out, "\r%c %s    (%s)", spinnerFrames[i%len(spinnerFrames)], s.label, FormatElapsed(time.Since(start)))
				s.mu.Unlock()
				i++
			}
		}
	}()
}

// Stop terminates the animation (if running) and writes the final status
// line. The line uses ✓ on success and ✗ on failure, matching the phase
// markers used elsewhere in the install output.
func (s *Spinner) Stop(success bool, elapsed time.Duration) {
	if s.live {
		close(s.stop)
		<-s.done
	}
	marker := "✓"
	if !success {
		marker = "✗"
	}
	fmt.Fprintf(s.out, "%s %s    (%s)\n", marker, s.label, FormatElapsed(elapsed))
}

// FormatElapsed prints a duration the way the install summary expects it:
// "12s" under a minute, "9m13s" otherwise. Rounding happens first so a
// duration like 59.6s renders as "1m00s" instead of an ambiguous "60s".
func FormatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Round(time.Second).Seconds())
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	return fmt.Sprintf("%dm%02ds", totalSeconds/60, totalSeconds%60)
}
