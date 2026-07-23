package keycloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

// newTestKeycloak stands up a minimal Keycloak Admin API stand-in that
// returns the given users payload on GET /admin/realms/{realm}/users and
// a static token on the client_credentials endpoint.
func newTestKeycloak(t *testing.T, users []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/test/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "stub", "expires_in": 60, "token_type": "Bearer"})
	})
	mux.HandleFunc("/admin/realms/test/users", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(users)
	})
	return httptest.NewServer(mux)
}

func TestListUsersFiltersFederatedUsersByDefault(t *testing.T) {
	server := newTestKeycloak(t, []map[string]any{
		{"id": "1", "username": "scim.user1", "enabled": true},
		{"id": "2", "username": "ldap.user1", "enabled": true, "federationLink": "abc-ldap"},
		{"id": "3", "username": "scim.user2", "enabled": true, "federationLink": ""},
	})
	defer server.Close()

	client, err := NewClient(config.KeycloakConfig{
		BaseURL:      server.URL,
		Realm:        "test",
		ClientID:     "cid",
		ClientSecret: "shh",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	users, err := client.ListUsers(context.Background(), ListUsersParams{Max: 50})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users (federated user filtered), got %d: %+v", len(users), users)
	}
	for _, u := range users {
		if strings.HasPrefix(u.Username, "ldap.") {
			t.Fatalf("LDAP-federated user leaked into result: %+v", u)
		}
	}
}

func TestListUsersIncludesFederatedWhenOptedIn(t *testing.T) {
	server := newTestKeycloak(t, []map[string]any{
		{"id": "1", "username": "scim.user1", "enabled": true},
		{"id": "2", "username": "ldap.user1", "enabled": true, "federationLink": "abc-ldap"},
	})
	defer server.Close()

	client, err := NewClient(config.KeycloakConfig{
		BaseURL:               server.URL,
		Realm:                 "test",
		ClientID:              "cid",
		ClientSecret:          "shh",
		IncludeFederatedUsers: true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	users, err := client.ListUsers(context.Background(), ListUsersParams{Max: 50})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected both users when IncludeFederatedUsers=true, got %d", len(users))
	}
	foundFederated := false
	for _, u := range users {
		if u.FederationLink == "abc-ldap" {
			foundFederated = true
		}
	}
	if !foundFederated {
		t.Fatalf("federated user's FederationLink not propagated to result: %+v", users)
	}
}
