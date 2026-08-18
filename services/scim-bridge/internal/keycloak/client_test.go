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

// newTestKeycloakGroups extends the stand-in with the groups and
// group-members endpoints; requests are captured so tests can assert
// query parameters.
func newTestKeycloakGroups(t *testing.T, groups []map[string]any, members map[string][]map[string]any, requests *[]*http.Request) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/test/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "stub", "expires_in": 60, "token_type": "Bearer"})
	})
	mux.HandleFunc("/admin/realms/test/groups", func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Clone(r.Context()))
		_ = json.NewEncoder(w).Encode(groups)
	})
	mux.HandleFunc("/admin/realms/test/groups/", func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Clone(r.Context()))
		groupID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/realms/test/groups/"), "/members")
		_ = json.NewEncoder(w).Encode(members[groupID])
	})
	return httptest.NewServer(mux)
}

func newGroupTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	client, err := NewClient(config.KeycloakConfig{
		BaseURL:      serverURL,
		Realm:        "test",
		ClientID:     "cid",
		ClientSecret: "shh",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
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

func TestListGroupsSearchesAndFlattensSubGroups(t *testing.T) {
	var requests []*http.Request
	server := newTestKeycloakGroups(t, []map[string]any{
		{
			"id": "g1", "name": "parent", "path": "/parent",
			"subGroups": []map[string]any{
				{"id": "g2", "name": "mas-scim-users", "path": "/parent/mas-scim-users"},
			},
		},
		{"id": "g3", "name": "mas-scim-users", "path": "/mas-scim-users"},
	}, nil, &requests)
	defer server.Close()

	client := newGroupTestClient(t, server.URL)
	groups, err := client.ListGroups(context.Background(), "mas-scim-users")
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(requests) != 1 || requests[0].URL.Query().Get("search") != "mas-scim-users" {
		t.Fatalf("expected one request with search=mas-scim-users, got %+v", requests)
	}
	if len(groups) != 3 {
		t.Fatalf("expected nested subgroup flattened into results, got %+v", groups)
	}
	byID := map[string]Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	if byID["g2"].Path != "/parent/mas-scim-users" || byID["g3"].Path != "/mas-scim-users" {
		t.Fatalf("unexpected group mapping: %+v", groups)
	}
}

func TestListGroupMembersPaginationParamsAndFederatedFilter(t *testing.T) {
	var requests []*http.Request
	server := newTestKeycloakGroups(t, nil, map[string][]map[string]any{
		"g1": {
			{"id": "1", "username": "scim.user1", "enabled": true},
			{"id": "2", "username": "ldap.user1", "enabled": true, "federationLink": "abc-ldap"},
			{"id": "3", "username": "scim.user2", "enabled": true, "federationLink": ""},
		},
	}, &requests)
	defer server.Close()

	client := newGroupTestClient(t, server.URL)
	users, rawCount, err := client.ListGroupMembers(context.Background(), "g1", 50, 50)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected one members request, got %d", len(requests))
	}
	q := requests[0].URL.Query()
	if q.Get("first") != "50" || q.Get("max") != "50" {
		t.Fatalf("expected first=50&max=50, got %s", requests[0].URL.RawQuery)
	}
	if len(users) != 2 {
		t.Fatalf("expected federated member filtered like ListUsers, got %d: %+v", len(users), users)
	}
	for _, u := range users {
		if strings.HasPrefix(u.Username, "ldap.") {
			t.Fatalf("LDAP-federated member leaked into result: %+v", u)
		}
	}
	// rawCount must reflect the pre-filter page size so a filter-thinned
	// page does not end pagination early.
	if rawCount != 3 {
		t.Fatalf("expected rawCount 3 (pre-filter), got %d", rawCount)
	}
}

func TestListGroupMembersIncludesFederatedWhenOptedIn(t *testing.T) {
	var requests []*http.Request
	server := newTestKeycloakGroups(t, nil, map[string][]map[string]any{
		"g1": {
			{"id": "1", "username": "scim.user1", "enabled": true},
			{"id": "2", "username": "ldap.user1", "enabled": true, "federationLink": "abc-ldap"},
		},
	}, &requests)
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

	users, rawCount, err := client.ListGroupMembers(context.Background(), "g1", 0, 50)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(users) != 2 || rawCount != 2 {
		t.Fatalf("expected both members when IncludeFederatedUsers=true, got %d (raw %d)", len(users), rawCount)
	}
}
