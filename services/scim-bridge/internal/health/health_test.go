package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestProbesFollowCycleOutcomes(t *testing.T) {
	c := &clock{t: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}
	tr := NewTrackerWithClock(25*time.Minute, c.now)
	h := tr.Handler()

	if rec := get(t, h, "/readyz"); rec.Code != 503 {
		t.Fatalf("readyz before first cycle = %d, want 503", rec.Code)
	}
	if rec := get(t, h, "/healthz"); rec.Code != 200 {
		t.Fatalf("healthz during start-up grace = %d, want 200", rec.Code)
	}

	// Failing cycles (e.g. Keycloak down) do not make it ready, and after
	// the stale window liveness fails.
	tr.RecordCycle(errors.New("keycloak: connection refused"))
	c.t = c.t.Add(26 * time.Minute)
	if rec := get(t, h, "/healthz"); rec.Code != 503 {
		t.Fatalf("healthz with no success in stale window = %d, want 503", rec.Code)
	}
	if rec := get(t, h, "/readyz"); rec.Code != 503 {
		t.Fatalf("readyz after only failures = %d, want 503", rec.Code)
	}

	tr.RecordCycle(nil)
	tr.SetEntryCounts(10, 2, 1)
	if rec := get(t, h, "/healthz"); rec.Code != 200 {
		t.Fatalf("healthz after success = %d", rec.Code)
	}
	if rec := get(t, h, "/readyz"); rec.Code != 200 {
		t.Fatalf("readyz after success = %d", rec.Code)
	}

	rec := get(t, h, "/status")
	var st Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("status JSON: %v (%s)", err, rec.Body.String())
	}
	if !st.Ready || !st.Healthy || st.ErrorEntries != 2 || st.ParkedEntries != 1 || st.TrackedEntries != 10 || st.CyclesFailed != 1 || st.CyclesSucceeded != 1 || st.LastSuccess.IsZero() {
		t.Fatalf("unexpected status %+v", st)
	}
}

func TestServeShutsDownOnCancel(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, NewTracker(time.Minute).Handler()) }()
	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET /healthz: %v %v", resp, err)
	}
	resp.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("server did not shut down")
	}
}
