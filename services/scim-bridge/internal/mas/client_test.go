package mas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestClient(rt http.RoundTripper) *Client {
	c, _ := NewClient(config.MASConfig{BaseURL: "https://example.com", ProfileID: "p", AuthType: "api-key", Token: "t"})
	c.http = &http.Client{Transport: rt, Timeout: 5 * time.Second}
	return c
}

func fastRetries(t *testing.T, retries int) {
	t.Helper()
	origBackoff, origRetries, origCap := baseBackoff, maxRetries, maxRetryAfter
	baseBackoff, maxRetries = time.Millisecond, retries
	t.Cleanup(func() { baseBackoff, maxRetries, maxRetryAfter = origBackoff, origRetries, origCap })
}

func resp(code int, body string, hdr http.Header) *http.Response {
	if hdr == nil {
		hdr = http.Header{}
	}
	return &http.Response{StatusCode: code, Status: http.StatusText(code), Body: io.NopCloser(bytes.NewBufferString(body)), Header: hdr}
}

func TestDoWithRetryRetriesIdempotent5xxThenSucceeds(t *testing.T) {
	fastRetries(t, 2)
	attempts := 0
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return resp(502, "bad", nil), nil
		}
		return resp(200, `{}`, nil), nil
	}))
	if err := client.UpdateUser(context.Background(), "p", "m1", UserResource{UserName: "alice"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

// A POST that got a 5xx may still have created the user; retrying could
// duplicate it, so the client must hand the 5xx back after one attempt.
func TestDoWithRetryDoesNotRetryPOSTOn5xx(t *testing.T) {
	fastRetries(t, 3)
	attempts := 0
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return resp(500, "", nil), nil
	}))
	_, err := client.CreateUser(context.Background(), "p", UserResource{UserName: "alice"})
	var respErr *ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != 500 {
		t.Fatalf("expected 500 ResponseError, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("POST must not be retried on 5xx, got %d attempts", attempts)
	}
}

func TestDoWithRetryDoesNotRetryPOSTOnTimeout(t *testing.T) {
	fastRetries(t, 3)
	attempts := 0
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return nil, context.DeadlineExceeded
	}))
	if _, err := client.CreateUser(context.Background(), "p", UserResource{UserName: "alice"}); err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Fatalf("POST must not be retried on timeout, got %d attempts", attempts)
	}
}

func TestDoWithRetryRetriesPOSTOnDialFailure(t *testing.T) {
	fastRetries(t, 2)
	attempts := 0
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
		}
		return resp(201, `{"id":"123"}`, nil), nil
	}))
	id, err := client.CreateUser(context.Background(), "p", UserResource{UserName: "alice"})
	if err != nil || id != "123" || attempts != 2 {
		t.Fatalf("expected dial failure retried, got id=%q err=%v attempts=%d", id, err, attempts)
	}
}

func TestDoWithRetryReturnsTransportErrorAfterBudget(t *testing.T) {
	fastRetries(t, 1)
	attempts := 0
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return nil, context.DeadlineExceeded
	}))
	err := client.UpdateUser(context.Background(), "p", "m1", UserResource{UserName: "alice"})
	var respErr *ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != 0 {
		t.Fatalf("expected ResponseError transport, got %v", err)
	}
	if attempts != 2 { // initial + retry
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestDoWithRetryHonoursRetryAfterOn429ForPOST(t *testing.T) {
	fastRetries(t, 2)
	var times []time.Time
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		times = append(times, time.Now())
		if len(times) == 1 {
			return resp(429, "slow down", http.Header{"Retry-After": []string{"1"}}), nil
		}
		return resp(201, `{"id":"abc"}`, nil), nil
	}))
	id, err := client.CreateUser(context.Background(), "p", UserResource{UserName: "alice"})
	if err != nil || id != "abc" {
		t.Fatalf("expected success after 429, got id=%q err=%v", id, err)
	}
	if gap := times[1].Sub(times[0]); gap < 900*time.Millisecond {
		t.Fatalf("Retry-After: 1 not honoured, retried after %s", gap)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	maxRetryAfter = 60 * time.Second
	if got := retryAfter("5", time.Second); got != 5*time.Second {
		t.Fatalf("seconds: %s", got)
	}
	if got := retryAfter("9999", time.Second); got != 60*time.Second {
		t.Fatalf("cap: %s", got)
	}
	if got := retryAfter("", 7*time.Millisecond); got != 7*time.Millisecond {
		t.Fatalf("fallback: %s", got)
	}
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	if got := retryAfter(future, time.Second); got < 8*time.Second || got > 11*time.Second {
		t.Fatalf("http-date: %s", got)
	}
}

func TestDoWithRetrySleepHonoursContextCancel(t *testing.T) {
	fastRetries(t, 3)
	baseBackoff = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		return resp(503, "", nil), nil
	}))
	done := make(chan error, 1)
	go func() { done <- client.UpdateUser(ctx, "p", "m1", UserResource{UserName: "alice"}) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("retry sleep ignored context cancellation")
	}
}

func TestFetchTokenUsesConfiguredClientAndContext(t *testing.T) {
	var gotAuth atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/authenticate" {
			http.NotFound(w, r)
			return
		}
		if u, p, ok := r.BasicAuth(); ok && u == "name" && p == "value" {
			gotAuth.Store(true)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "jwt.token.value"})
	}))
	defer srv.Close()

	// Without trusting the test server's CA the call must fail: proves the
	// TLS config is the configured one, not a permissive default.
	if _, err := FetchToken(context.Background(), config.MASConfig{BaseURL: srv.URL + "/scim/v2"}, "name", "value"); err == nil {
		t.Fatalf("expected TLS failure without CA")
	}
	tok, err := FetchToken(context.Background(), config.MASConfig{BaseURL: srv.URL + "/scim/v2", InsecureSkipVerify: true}, "name", "value")
	if err != nil || tok != "jwt.token.value" || !gotAuth.Load() {
		t.Fatalf("FetchToken: tok=%q err=%v auth=%v", tok, err, gotAuth.Load())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchToken(ctx, config.MASConfig{BaseURL: srv.URL, InsecureSkipVerify: true}, "name", "value"); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancelled context to abort FetchToken, got %v", err)
	}
}

func TestEmptyMASIDNeverAddressesTheCollection(t *testing.T) {
	calls := 0
	client := newTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return resp(204, "", nil), nil
	}))
	if err := client.DeleteUser(context.Background(), "p", ""); err == nil {
		t.Fatalf("expected error for empty MAS ID")
	}
	if err := client.PatchUser(context.Background(), "p", " ", []PatchOperation{{Op: "replace", Path: "active", Value: false}}); err == nil {
		t.Fatalf("expected error for blank MAS ID")
	}
	if calls != 0 {
		t.Fatalf("no request may be sent for an empty MAS ID, got %d", calls)
	}
}

func TestFetchTokenRejectsUnreadableCAFile(t *testing.T) {
	_, err := FetchToken(context.Background(), config.MASConfig{BaseURL: "https://mas.example", CAFile: "/nonexistent/ca.crt"}, "a", "b")
	if err == nil || !strings.Contains(err.Error(), "CA") {
		t.Fatalf("expected MAS_CA_FILE to be honoured (and fail), got %v", err)
	}
}
