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
}

// Option configures the client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the default http.Client. Useful for tests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithInsecureTLS disables TLS verification. Cluster routes are commonly
// signed by a cluster-local CA that isn't in the system trust store.
func WithInsecureTLS() Option {
	return func(c *Client) {
		c.httpClient = &http.Client{
			Timeout: c.httpClient.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
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
		baseURL:    u,
		tokenName:  tokenName,
		tokenValue: tokenValue,
		httpClient: &http.Client{Timeout: 60 * time.Second},
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
		return "", fmt.Errorf("masadmin: authenticate against %s: %w", authURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("masadmin: authenticate against %s: HTTP %d: %s", authURL, resp.StatusCode, strings.TrimSpace(string(body)))
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
// On 401 the cached JWT is dropped and the request is retried once.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doWithRetry(ctx, func() (*http.Request, error) {
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
// header. On 401 the cached JWT is dropped and the request is retried once.
func (c *Client) doMultipart(ctx context.Context, method, path string, body []byte, contentType string, out any) error {
	return c.doWithRetry(ctx, func() (*http.Request, error) {
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

func (c *Client) doWithRetry(ctx context.Context, build func() (*http.Request, error), out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := build()
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("masadmin: %s %s: %w", req.Method, req.URL.Path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("masadmin: read %s %s response: %w", req.Method, req.URL.Path, readErr)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.ResetToken()
			continue
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
		return nil
	}
	return fmt.Errorf("masadmin: exhausted retries")
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
