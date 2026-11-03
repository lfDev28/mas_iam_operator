package masadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newFastRetryClient(t *testing.T, srv *httptest.Server, attempts int) *Client {
	t.Helper()
	c, err := NewClient(srv.URL, "tester", "secret", WithHTTPClient(srv.Client()), WithRetryPolicy(attempts, time.Millisecond))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestRetriesTransientStatusForIdempotentMethods(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var getHits, putHits int32
			srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
				counter := &getHits
				if r.Method == http.MethodPut {
					counter = &putHits
				}
				if atomic.AddInt32(counter, 1) < 3 {
					w.WriteHeader(status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"metadata":{},"status":{}}`))
			})
			defer srv.Close()
			c := newFastRetryClient(t, srv, 5)
			if _, err := c.GetOIDC(context.Background(), "default"); err != nil {
				t.Fatalf("GetOIDC: %v", err)
			}
			if _, err := c.SetOIDC(context.Background(), "default", OIDCConfigRequest{ClientID: "x", ClientSecret: "y"}); err != nil {
				t.Fatalf("SetOIDC: %v", err)
			}
			if got := atomic.LoadInt32(&getHits); got != 3 {
				t.Errorf("GET hits = %d, want 3", got)
			}
			if got := atomic.LoadInt32(&putHits); got != 3 {
				t.Errorf("PUT hits = %d, want 3", got)
			}
		})
	}
}

func TestDoesNotRetryPost(t *testing.T) {
	var hits int32
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer srv.Close()
	c := newFastRetryClient(t, srv, 5)
	if _, err := c.TestLDAP(context.Background(), LDAPConfigRequest{URL: "ldaps://x"}); err == nil {
		t.Fatal("expected error from 503 POST")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("POST hits = %d, want exactly 1 (POST is not idempotent)", got)
	}
}

func TestDoesNotRetryNonTransientStatus(t *testing.T) {
	var hits int32
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()
	c := newFastRetryClient(t, srv, 5)
	if _, err := c.GetLDAP(context.Background(), "default"); err == nil {
		t.Fatal("expected error from 500")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("hits = %d, want 1 (500 is not a transient status)", got)
	}
}

func TestRetryGivesUpAfterMaxAttempts(t *testing.T) {
	var hits int32
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
	})
	defer srv.Close()
	c := newFastRetryClient(t, srv, 3)
	_, err := c.GetSAML(context.Background(), "default")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway {
		t.Fatalf("err = %v, want wrapped APIError 502", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("hits = %d, want 3", got)
	}
}

func TestRetriesNetworkErrorsForGet(t *testing.T) {
	var hits int32
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			// Simulate a dropped connection: hijack and close without a response.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("server does not support hijacking")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{},"status":{}}`))
	})
	defer srv.Close()
	c := newFastRetryClient(t, srv, 3)
	if _, err := c.GetLDAP(context.Background(), "default"); err != nil {
		t.Fatalf("GetLDAP: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits = %d, want 2", got)
	}
}

func TestRetriesTransientAuthenticateFailure(t *testing.T) {
	var authHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/authenticate", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&authHits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "jwt"})
	})
	mux.HandleFunc("/config/ldap/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{},"status":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newFastRetryClient(t, srv, 3)
	if _, err := c.GetLDAP(context.Background(), "default"); err != nil {
		t.Fatalf("GetLDAP: %v", err)
	}
	if got := atomic.LoadInt32(&authHits); got != 2 {
		t.Errorf("auth hits = %d, want 2", got)
	}
}

func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		hint    time.Duration
		want    time.Duration
	}{
		{1, 0, 2 * time.Second},
		{2, 0, 4 * time.Second},
		{3, 0, 8 * time.Second},
		{10, 0, maxRetryDelay},
		{1, 5 * time.Second, 5 * time.Second},
		{1, time.Hour, maxRetryDelay},
	}
	for _, tc := range cases {
		if got := backoffDelay(2*time.Second, tc.attempt, tc.hint); got != tc.want {
			t.Errorf("backoffDelay(attempt=%d, hint=%s) = %s, want %s", tc.attempt, tc.hint, got, tc.want)
		}
	}
}

func TestIDPResponseRecordsServerTime(t *testing.T) {
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Wed, 04 Nov 2026 10:00:00 GMT")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"generation":2},"status":{"conditions":[{"type":"Ready","status":"True","lastTransitionTime":"2026-11-04T09:00:00Z"}]}}`))
	})
	defer srv.Close()
	resp, err := newFastRetryClient(t, srv, 1).SetLDAP(context.Background(), "default", LDAPConfigRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	if !resp.ServerTime.Equal(want) {
		t.Fatalf("ServerTime = %s, want %s", resp.ServerTime, want)
	}
	if resp.Metadata.Generation != 2 {
		t.Fatalf("generation = %d, want 2", resp.Metadata.Generation)
	}
	if got := resp.ReadyAfter(resp.ServerTime, 5*time.Second); got != ReadyStale {
		t.Fatalf("ReadyAfter = %v, want ReadyStale (Ready predates the PUT)", got)
	}
}

func TestInsecureTransportHonoursProxyEnv(t *testing.T) {
	if NewInsecureTransport().Proxy == nil {
		t.Fatal("NewInsecureTransport must set Proxy (http.ProxyFromEnvironment); a nil Proxy ignores HTTPS_PROXY")
	}
	c, err := NewClient("https://api.example.com", "n", "v", WithInsecureTLS())
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("WithInsecureTLS transport must honour proxy env vars")
	}
}
