// Package health exposes the bridge's liveness, readiness and status over
// HTTP for Kubernetes probes and support.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Status is the /status JSON document.
type Status struct {
	Healthy             bool      `json:"healthy"`
	Ready               bool      `json:"ready"`
	StartedAt           time.Time `json:"startedAt"`
	LastAttempt         time.Time `json:"lastAttempt,omitzero"`
	LastSuccess         time.Time `json:"lastSuccess,omitzero"`
	LastError           string    `json:"lastError,omitempty"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
	CyclesSucceeded     int       `json:"cyclesSucceeded"`
	CyclesFailed        int       `json:"cyclesFailed"`
	StaleAfter          string    `json:"staleAfter"`
	TrackedEntries      int       `json:"trackedEntries"`
	ErrorEntries        int       `json:"errorEntries"`
	ParkedEntries       int       `json:"parkedEntries"`
}

// Tracker records poll-cycle outcomes. It is safe for concurrent use.
type Tracker struct {
	mu         sync.Mutex
	now        func() time.Time
	staleAfter time.Duration
	st         Status
}

// NewTracker returns a tracker that reports unhealthy once no cycle has
// succeeded for staleAfter (counted from start until the first success).
func NewTracker(staleAfter time.Duration) *Tracker {
	return NewTrackerWithClock(staleAfter, time.Now)
}

// NewTrackerWithClock is NewTracker with an injectable clock (tests).
func NewTrackerWithClock(staleAfter time.Duration, now func() time.Time) *Tracker {
	return &Tracker{now: now, staleAfter: staleAfter, st: Status{StartedAt: now(), StaleAfter: staleAfter.String()}}
}

// RecordCycle records the outcome of one reconciliation cycle.
func (t *Tracker) RecordCycle(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.st.LastAttempt = now
	if err != nil {
		t.st.LastError = err.Error()
		t.st.ConsecutiveFailures++
		t.st.CyclesFailed++
		return
	}
	t.st.LastSuccess = now
	t.st.LastError = ""
	t.st.ConsecutiveFailures = 0
	t.st.CyclesSucceeded++
	t.st.Ready = true
}

// SetEntryCounts records state-store counters for /status.
func (t *Tracker) SetEntryCounts(tracked, errored, parked int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st.TrackedEntries = tracked
	t.st.ErrorEntries = errored
	t.st.ParkedEntries = parked
}

// Snapshot returns the current status.
func (t *Tracker) Snapshot() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.st
	since := st.LastSuccess
	if since.IsZero() {
		since = st.StartedAt
	}
	st.Healthy = t.now().Sub(since) <= t.staleAfter
	return st
}

// Handler serves /healthz (liveness: a cycle succeeded recently), /readyz
// (a first cycle has succeeded) and /status (JSON counters).
func (t *Tracker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		st := t.Snapshot()
		if !st.Healthy {
			http.Error(w, "no successful sync cycle within "+st.StaleAfter, http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !t.Snapshot().Ready {
			http.Error(w, "first sync cycle not completed", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(t.Snapshot())
	})
	return mux
}

// Listen binds addr; a bind failure is a startup configuration error.
func Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Serve serves h on ln until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
