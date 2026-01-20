package mas

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

// Client is an HTTP adapter for MAS SCIM endpoints.
type Client struct {
	cfg  config.MASConfig
	base string
	http *http.Client
}

// User is a minimal SCIM user representation for search results.
type User struct {
	ID         string `json:"id"`
	ExternalID string `json:"externalId,omitempty"`
	UserName   string `json:"userName,omitempty"`
}

// PatchOperation represents a SCIM PatchOp entry.
type PatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

// ResponseError captures MAS SCIM HTTP error details.
type ResponseError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("MAS SCIM request failed: %s: %s", e.Status, e.Body)
}

// NewClient builds a MAS SCIM client using the provided configuration.
func NewClient(cfg config.MASConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("mas base URL missing")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid mas base URL: %w", err)
	}
	trimmed := strings.TrimRight(cfg.BaseURL, "/")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{}
	if cfg.CAFile != "" {
		rootCAs, err := loadCertPool(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("load MAS CA file: %w", err)
		}
		tlsConfig.RootCAs = rootCAs
	}
	if cfg.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true // dev/self-signed only
	}
	if tlsConfig.RootCAs != nil || tlsConfig.InsecureSkipVerify {
		transport.TLSClientConfig = tlsConfig
	}
	return &Client{
		cfg:  cfg,
		base: trimmed,
		http: &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}, nil
}

// DefaultProfileID returns the configured default MAS SCIM profile.
func (c *Client) DefaultProfileID() string {
	return c.cfg.ProfileID
}

// BaseURL exposes the normalized MAS SCIM endpoint root.
func (c *Client) BaseURL() string {
	return c.base
}

// CreateUser issues a SCIM POST and returns the MAS resource ID.
func (c *Client) CreateUser(ctx context.Context, profileID string, payload UserResource) (string, error) {
	endpoint, err := c.usersEndpoint(profileID)
	if err != nil {
		return "", err
	}
	req, err := c.newRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return "", err
	}
	resp, err := c.doWithRetry(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", c.errorFromResponse(resp)
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode MAS response: %w", err)
	}
	if body.ID == "" {
		return "", fmt.Errorf("MAS response missing id")
	}
	return body.ID, nil
}

// UpdateUser issues a SCIM PUT for an existing MAS resource.
func (c *Client) UpdateUser(ctx context.Context, profileID, masID string, payload UserResource) error {
	endpoint, err := c.userEndpoint(profileID, masID)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPut, endpoint, payload)
	if err != nil {
		return err
	}
	resp, err := c.doWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.errorFromResponse(resp)
	}
	return nil
}

// DeleteUser issues a SCIM DELETE for an existing MAS resource.
func (c *Client) DeleteUser(ctx context.Context, profileID, masID string) error {
	endpoint, err := c.userEndpoint(profileID, masID)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.doWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return c.errorFromResponse(resp)
	}
	return nil
}

// PatchUser issues a SCIM PATCH for an existing MAS resource.
func (c *Client) PatchUser(ctx context.Context, profileID, masID string, ops []PatchOperation) error {
	if len(ops) == 0 {
		return nil
	}
	endpoint, err := c.userEndpoint(profileID, masID)
	if err != nil {
		return err
	}
	payload := struct {
		Schemas    []string         `json:"schemas"`
		Operations []PatchOperation `json:"Operations"`
	}{
		Schemas:    []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		Operations: ops,
	}
	req, err := c.newRequest(ctx, http.MethodPatch, endpoint, payload)
	if err != nil {
		return err
	}
	resp, err := c.doWithRetry(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.errorFromResponse(resp)
	}
	return nil
}

// SearchUsers issues a SCIM GET with a filter and returns matches.
func (c *Client) SearchUsers(ctx context.Context, profileID, filter string) ([]User, error) {
	endpoint, err := c.usersEndpoint(profileID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("filter", filter)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", c.authHeader())

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.errorFromResponse(resp)
	}
	var payload struct {
		TotalResults int    `json:"totalResults"`
		Resources    []User `json:"Resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode MAS search response: %w", err)
	}
	return payload.Resources, nil
}

func (c *Client) usersEndpoint(profileID string) (string, error) {
	if profileID == "" {
		return "", fmt.Errorf("profile ID required for MAS SCIM call")
	}
	return url.JoinPath(c.base, profileID, "Users")
}

func (c *Client) userEndpoint(profileID, id string) (string, error) {
	if profileID == "" {
		return "", fmt.Errorf("profile ID required for MAS SCIM call")
	}
	return url.JoinPath(c.base, profileID, "Users", id)
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, body any) (*http.Request, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode SCIM payload: %w", err)
		}
	}
	data := buf.Bytes()
	reader := bytes.NewReader(data)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/scim+json")
	}
	req.Header.Set("Authorization", c.authHeader())
	return req, nil
}

func (c *Client) errorFromResponse(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return &ResponseError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       strings.TrimSpace(string(body)),
	}
}

func (c *Client) authHeader() string {
	switch strings.ToLower(c.cfg.AuthType) {
	case "api-key":
		return "APIKey " + c.cfg.Token
	default:
		return "Bearer " + c.cfg.Token
	}
}

// retry settings (override in tests as needed)
var (
	maxRetries  = 3
	baseBackoff = 200 * time.Millisecond
)

func jitterDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	jitter := rand.Int63n(int64(d / 2))
	return d + time.Duration(jitter)
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	backoff := baseBackoff
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if !isRetryableError(err) || attempt == maxRetries {
				return nil, &ResponseError{StatusCode: 0, Status: "transport error", Body: err.Error()}
			}
			log.Printf("MAS request retry attempt=%d method=%s url=%s error=%v", attempt, req.Method, req.URL.String(), err)
			time.Sleep(jitterDuration(backoff))
			backoff *= 2
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = &ResponseError{StatusCode: resp.StatusCode, Status: resp.Status}
			if attempt == maxRetries {
				return resp, nil
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			log.Printf("MAS request retry attempt=%d method=%s url=%s status=%s", attempt, req.Method, req.URL.String(), resp.Status)
			time.Sleep(jitterDuration(backoff))
			backoff *= 2
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

func isRetryableError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return false
}

func loadCertPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("failed to append certs from %s", path)
	}
	return pool, nil
}
