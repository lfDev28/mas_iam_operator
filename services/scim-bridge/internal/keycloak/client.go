package keycloak

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

// Client interacts with the Keycloak Admin REST API.
type Client struct {
	cfg  config.KeycloakConfig
	http *http.Client
	base string
}

// NewClient returns an HTTP-backed Keycloak client.
func NewClient(cfg config.KeycloakConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("keycloak base URL missing")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid keycloak base URL: %w", err)
	}
	trimmed := strings.TrimRight(cfg.BaseURL, "/")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{}
	if cfg.CAFile != "" {
		rootCAs, err := loadCertPool(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("load keycloak CA file: %w", err)
		}
		tlsConfig.RootCAs = rootCAs
	}
	if cfg.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}
	if tlsConfig.InsecureSkipVerify || tlsConfig.RootCAs != nil {
		transport.TLSClientConfig = tlsConfig
	}
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second, Transport: transport},
		base: trimmed,
	}, nil
}

// Realm returns the configured realm identifier.
func (c *Client) Realm() string {
	return c.cfg.Realm
}

// BaseURL exposes the configured Keycloak endpoint.
func (c *Client) BaseURL() string {
	return c.base
}

// ListUsersParams controls pagination when listing users.
type ListUsersParams struct {
	First int
	Max   int
}

// User contains the fields we need from the Keycloak Admin API.
type User struct {
	ID         string
	Username   string
	Email      string
	Enabled    bool
	FirstName  string
	LastName   string
	Attributes map[string][]string
	MasProfile string
	// FederationLink identifies the user federation provider that owns
	// this user (e.g. an LDAP provider). Non-empty means the user was
	// imported from a federation source rather than created directly in
	// Keycloak. The bridge defaults to filtering these users out so
	// LDAP-imported accounts aren't double-created via SCIM self-reg —
	// see KeycloakConfig.IncludeFederatedUsers for the override.
	FederationLink string
}

// ListUsers fetches a single page of users from Keycloak.
func (c *Client) ListUsers(ctx context.Context, params ListUsersParams) ([]User, error) {
	token, err := c.clientCredentialsToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(c.base, "admin", "realms", c.cfg.Realm, "users")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	if params.First > 0 {
		q.Set("first", strconv.Itoa(params.First))
	}
	if params.Max > 0 {
		q.Set("max", strconv.Itoa(params.Max))
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		bodyText := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusBadRequest && params.Max > 1 && strings.Contains(bodyText, "Cannot parse the JSON") {
			fallback := params
			fallback.Max = 1
			return c.ListUsers(ctx, fallback)
		}
		return nil, fmt.Errorf("keycloak list users: %s: %s", resp.Status, bodyText)
	}
	var payload []struct {
		ID             string              `json:"id"`
		Username       string              `json:"username"`
		Email          string              `json:"email"`
		Enabled        bool                `json:"enabled"`
		FirstName      string              `json:"firstName"`
		LastName       string              `json:"lastName"`
		Attributes     map[string][]string `json:"attributes"`
		FederationLink string              `json:"federationLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	users := make([]User, 0, len(payload))
	for _, u := range payload {
		// Skip users owned by a user-federation provider (e.g. LDAP) unless
		// the operator has explicitly opted in. Without this filter the
		// bridge re-syncs LDAP-imported users into MAS via SCIM, which
		// double-creates them and breaks LDAP self-reg with a 409 conflict.
		if !c.cfg.IncludeFederatedUsers && strings.TrimSpace(u.FederationLink) != "" {
			continue
		}
		attrs := normalizeAttributes(u.Attributes)
		users = append(users, User{
			ID:             u.ID,
			Username:       u.Username,
			Email:          u.Email,
			Enabled:        u.Enabled,
			FirstName:      u.FirstName,
			LastName:       u.LastName,
			Attributes:     attrs,
			MasProfile:     firstAttribute(attrs, "masProfile"),
			FederationLink: u.FederationLink,
		})
	}
	return users, nil
}

func (c *Client) clientCredentialsToken(ctx context.Context) (string, error) {
	endpoint, err := url.JoinPath(c.base, "realms", c.cfg.Realm, "protocol", "openid-connect", "token")
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("keycloak token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if payload.AccessToken == "" {
		return "", errors.New("token response missing access_token")
	}
	return payload.AccessToken, nil
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

func firstAttribute(attrs map[string][]string, key string) string {
	values, ok := attrs[key]
	if !ok || len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return values[0]
}

func normalizeAttributes(attrs map[string][]string) map[string][]string {
	if attrs == nil {
		return map[string][]string{}
	}
	return attrs
}
