// Package masadmin is a typed client for the MAS Admin APIs that this CLI
// uses to configure external identity providers (LDAP, OIDC, SAML).
//
// Endpoints live on the MAS API host (the same host that serves SCIM, just
// without the /scim/v2 prefix). Auth is a JWT minted via basic auth against
// /v1/authenticate and sent in the x-access-token header.
package masadmin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to a single MAS instance's Admin API.
type Client struct {
	baseURL    *url.URL
	tokenName  string
	tokenValue string
	httpClient *http.Client

	mu         sync.Mutex
	cachedJWT  string
	jwtExpires time.Time

	// maxAttempts / retryBaseDelay bound the transient-failure retry loop in
	// doWithRetry. See WithRetryPolicy.
	maxAttempts    int
	retryBaseDelay time.Duration
}

const (
	defaultMaxAttempts    = 5
	defaultRetryBaseDelay = 2 * time.Second
	maxRetryDelay         = 30 * time.Second
)

// Option configures the client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the default http.Client. Useful for tests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithInsecureTLS disables TLS verification. Cluster routes are commonly
// signed by a cluster-local CA that isn't in the system trust store.
//
// Proxy is set explicitly: a hand-built http.Transport does NOT inherit
// http.DefaultTransport's ProxyFromEnvironment, so without it HTTPS_PROXY /
// NO_PROXY were silently ignored on proxied networks.
func WithInsecureTLS() Option {
	return func(c *Client) {
		c.httpClient = &http.Client{
			Timeout:   c.httpClient.Timeout,
			Transport: NewInsecureTransport(),
		}
	}
}

// NewInsecureTransport returns an http.Transport that skips TLS verification
// (MAS and Keycloak routes are commonly signed by a cluster-local CA) and
// honours HTTPS_PROXY / HTTP_PROXY / NO_PROXY from the environment.
func NewInsecureTransport() *http.Transport {
	return &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

// WithRetryPolicy overrides how many times an idempotent request (GET, HEAD,
// PUT) is attempted on a transient failure — a network error or HTTP 429,
// 502, 503 or 504 — and the base of the exponential backoff between attempts.
// maxAttempts < 1 disables retries. Mostly for tests, which want a tiny delay.
func WithRetryPolicy(maxAttempts int, baseDelay time.Duration) Option {
	return func(c *Client) {
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		c.maxAttempts = maxAttempts
		c.retryBaseDelay = baseDelay
	}
}

// NewClient returns a Client that talks to baseURL using the provided
// API-key credentials (the same pair used by the SCIM bridge). baseURL must
// be the MAS API root, with or without a trailing slash and with no path —
// e.g. https://api.mas91.apps.example.com. The /scim/v2 suffix is stripped if
// present.
func NewClient(baseURL, tokenName, tokenValue string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("masadmin: baseURL is required")
	}
	root := StripSCIMSuffix(baseURL)
	u, err := url.Parse(root)
	if err != nil {
		return nil, fmt.Errorf("masadmin: parse baseURL %q: %w", root, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("masadmin: baseURL %q must include scheme and host", root)
	}
	if tokenName == "" || tokenValue == "" {
		return nil, errors.New("masadmin: tokenName and tokenValue are required")
	}
	c := &Client{
		baseURL:        u,
		tokenName:      tokenName,
		tokenValue:     tokenValue,
		httpClient:     &http.Client{Timeout: 60 * time.Second},
		maxAttempts:    defaultMaxAttempts,
		retryBaseDelay: defaultRetryBaseDelay,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL returns the API root URL the client targets.
func (c *Client) BaseURL() string {
	return c.baseURL.String()
}

// StripSCIMSuffix removes a trailing "/scim/v2" (and any trailing slash) from
// raw, returning the API root. It is exported so callers can derive a root
// from an existing MASBaseURL config value without round-tripping the client.
func StripSCIMSuffix(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	trimmed = strings.TrimSuffix(trimmed, "/scim/v2")
	return strings.TrimRight(trimmed, "/")
}

// Authenticate mints a fresh JWT via /v1/authenticate using basic auth and
// caches it for the lifetime of the process. Successive calls return the
// cached token until ResetToken is invoked.
func (c *Client) Authenticate(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.cachedJWT != "" && (c.jwtExpires.IsZero() || time.Now().Before(c.jwtExpires)) {
		token := c.cachedJWT
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	authURL := c.baseURL.ResolveReference(&url.URL{Path: "/v1/authenticate"}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.tokenName, c.tokenValue)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", &transientError{err: fmt.Errorf("masadmin: authenticate against %s: %w", authURL, err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("masadmin: authenticate against %s: HTTP %d: %s", authURL, resp.StatusCode, strings.TrimSpace(string(body)))
		if isTransientStatus(resp.StatusCode) {
			return "", &transientError{err: err, retryAfter: retryAfter(resp)}
		}
		return "", err
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("masadmin: decode authenticate response: %w", err)
	}
	if payload.Token == "" {
		return "", fmt.Errorf("masadmin: authenticate response from %s contained no token", authURL)
	}

	c.mu.Lock()
	c.cachedJWT = payload.Token
	c.jwtExpires = time.Time{}
	c.mu.Unlock()
	return payload.Token, nil
}

// ResetToken drops the cached JWT so the next call mints a fresh one.
func (c *Client) ResetToken() {
	c.mu.Lock()
	c.cachedJWT = ""
	c.jwtExpires = time.Time{}
	c.mu.Unlock()
}

// doJSON issues an authenticated JSON request. If body is non-nil it is
// JSON-marshalled. If out is non-nil the response body is decoded into it.
// On 401 the cached JWT is dropped and the request is retried once;
// transient failures are retried with backoff for idempotent methods (see
// doWithRetry).
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doWithRetry(ctx, method, func() (*http.Request, error) {
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(raw)
		}
		req, err := c.newRequest(ctx, method, path, reader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		return req, nil
	}, out)
}

// doMultipart issues an authenticated multipart/form-data request. body must
// already be encoded; contentType must be the boundary-bearing Content-Type
// header. Retries as doJSON.
func (c *Client) doMultipart(ctx context.Context, method, path string, body []byte, contentType string, out any) error {
	return c.doWithRetry(ctx, method, func() (*http.Request, error) {
		req, err := c.newRequest(ctx, method, path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept", "application/json")
		return req, nil
	}, out)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u := c.baseURL.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	token, err := c.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-access-token", token)
	return req, nil
}

// doWithRetry runs one logical request. Two independent retry budgets apply:
//
//   - 401: the cached JWT is dropped and the request is retried once, for any
//     method (the request was rejected before it did anything).
//   - transient failures — a network error, or HTTP 429/502/503/504 — are
//     retried with exponential backoff up to c.maxAttempts, but ONLY for
//     idempotent methods (GET, HEAD, PUT). A POST that timed out may already
//     have taken effect, so it is never replayed. A transient failure while
//     minting the JWT is retried for every method: the target request was
//     never sent.
func (c *Client) doWithRetry(ctx context.Context, method string, build func() (*http.Request, error), out any) error {
	maxAttempts := c.maxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	retryable := isIdempotent(method)
	unauthorizedRetried := false
	for attempt := 1; ; attempt++ {
		req, err := build()
		if err != nil {
			var transient *transientError
			if errors.As(err, &transient) && attempt < maxAttempts && ctx.Err() == nil {
				if c.backoff(ctx, attempt, transient.retryAfter) == nil {
					continue
				}
			}
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			wrapped := fmt.Errorf("masadmin: %s %s: %w", req.Method, req.URL.Path, err)
			if retryable && attempt < maxAttempts && ctx.Err() == nil {
				if c.backoff(ctx, attempt, 0) == nil {
					continue
				}
			}
			return wrapped
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			wrapped := fmt.Errorf("masadmin: read %s %s response: %w", req.Method, req.URL.Path, readErr)
			if retryable && attempt < maxAttempts && ctx.Err() == nil {
				if c.backoff(ctx, attempt, 0) == nil {
					continue
				}
			}
			return wrapped
		}
		if resp.StatusCode == http.StatusUnauthorized && !unauthorizedRetried {
			unauthorizedRetried = true
			c.ResetToken()
			// The 401 retry does not consume the transient budget.
			attempt--
			continue
		}
		if retryable && isTransientStatus(resp.StatusCode) && attempt < maxAttempts {
			if c.backoff(ctx, attempt, retryAfter(resp)) == nil {
				continue
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return decodeAPIError(req.Method, req.URL.Path, resp.StatusCode, body)
		}
		if out == nil || len(body) == 0 {
			return nil
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("masadmin: decode %s %s response: %w", req.Method, req.URL.Path, err)
		}
		if stamped, ok := out.(serverTimeSetter); ok {
			if date, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
				stamped.setServerTime(date)
			}
		}
		return nil
	}
}

// serverTimeSetter is implemented by response types that record the
// server's Date header (see IDPConfigResponse.ServerTime).
type serverTimeSetter interface {
	setServerTime(time.Time)
}

// transientError marks a failure worth retrying (network error, 429, 502,
// 503, 504). retryAfter carries a server-supplied Retry-After, if any.
type transientError struct {
	err        error
	retryAfter time.Duration
}

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut:
		return true
	}
	return false
}

func isTransientStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter parses a Retry-After header given in seconds. The HTTP-date form
// is ignored; 0 means "use the exponential backoff".
func retryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// backoffDelay is the wait before retry number `attempt` (1-based): base,
// 2*base, 4*base, … capped at maxRetryDelay. A server Retry-After hint wins
// (also capped).
func backoffDelay(base time.Duration, attempt int, hint time.Duration) time.Duration {
	delay := base
	for i := 1; i < attempt && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	if hint > 0 {
		delay = hint
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// backoff sleeps for backoffDelay, returning ctx.Err() if the context ends
// first.
func (c *Client) backoff(ctx context.Context, attempt int, hint time.Duration) error {
	delay := backoffDelay(c.retryBaseDelay, attempt, hint)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func decodeAPIError(method, path string, status int, body []byte) error {
	apiErr := &APIError{Status: status}
	if len(body) > 0 {
		_ = json.Unmarshal(body, apiErr)
	}
	if apiErr.Message == "" {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		apiErr.Message = snippet
	}
	return fmt.Errorf("masadmin: %s %s: %w", method, path, apiErr)
}

// Error implements error so callers can errors.As(err, &APIError{}) and inspect Status.
func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// IsNotFound reports whether err is a wrapped APIError with status 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusNotFound
	}
	return false
}
