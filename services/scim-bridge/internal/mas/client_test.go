package mas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
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

func TestDoWithRetryRetriesOn5xxThenSucceeds(t *testing.T) {
	origBackoff := baseBackoff
	origRetries := maxRetries
	baseBackoff = 1 * time.Millisecond
	maxRetries = 2
	defer func() {
		baseBackoff = origBackoff
		maxRetries = origRetries
	}()
	attempts := 0
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: 502, Status: "502 Bad Gateway", Body: io.NopCloser(bytes.NewBufferString("bad")), Header: http.Header{}}, nil
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewBufferString(`{"id":"123"}`)), Header: http.Header{}}, nil
	})

	client := newTestClient(rt)
	id, err := client.CreateUser(context.Background(), "p", UserResource{UserName: "alice"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if id != "123" {
		t.Fatalf("unexpected id %s", id)
	}
}

func TestDoWithRetryReturnsTransportErrorAfterBudget(t *testing.T) {
	origBackoff := baseBackoff
	origRetries := maxRetries
	baseBackoff = 1 * time.Millisecond
	maxRetries = 1
	defer func() {
		baseBackoff = origBackoff
		maxRetries = origRetries
	}()
	attempts := 0
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return nil, context.DeadlineExceeded
	})
	client := newTestClient(rt)
	_, err := client.CreateUser(context.Background(), "p", UserResource{UserName: "alice"})
	if err == nil {
		t.Fatalf("expected error")
	}
	var respErr *ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != 0 {
		t.Fatalf("expected ResponseError transport, got %v", err)
	}
	if attempts != 2 { // initial + retry
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}
