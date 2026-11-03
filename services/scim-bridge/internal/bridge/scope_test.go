package bridge

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
)

// Without group scoping the bridge used to read one 50-user page.
func TestPollerRealmModePagesThroughAllUsers(t *testing.T) {
	users := make([]keycloak.User, 0, 250)
	for i := 0; i < 250; i++ {
		users = append(users, keycloak.User{ID: fmt.Sprintf("u%03d", i), Username: fmt.Sprintf("scim.user%03d", i)})
	}
	kc := &fakeScopeKC{users: users}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "", nil)
	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.created) != 250 {
		t.Fatalf("expected all 250 realm users synced, got %d", len(masClient.created))
	}
	if kc.listUsersCalls != 3 {
		t.Fatalf("expected 3 pages of 100, got %d calls", kc.listUsersCalls)
	}
}

// Keycloak lowercases usernames; config written with capitals must match.
func TestScopeFiltersAreCaseInsensitive(t *testing.T) {
	kc := &fakeScopeKC{users: []keycloak.User{
		{ID: "u1", Username: "scim.user1"},
		{ID: "u2", Username: "scim.user2"},
		{ID: "u3", Username: "oidc.user1"},
	}}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "SCIM.", nil)
	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sort.Strings(masClient.created)
	if len(masClient.created) != 2 || masClient.created[0] != "scim.user1" || masClient.created[1] != "scim.user2" {
		t.Fatalf("expected prefix SCIM. to match scim.*, got %v", masClient.created)
	}

	masClient2 := &recordingMAS{}
	p2 := newTestPoller(t, kc, masClient2, []string{"Scim.User2", "OIDC.USER1"}, "", nil)
	if err := p2.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sort.Strings(masClient2.created)
	if len(masClient2.created) != 2 || masClient2.created[0] != "oidc.user1" || masClient2.created[1] != "scim.user2" {
		t.Fatalf("expected case-insensitive username allowlist, got %v", masClient2.created)
	}
}
