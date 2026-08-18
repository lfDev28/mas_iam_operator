package bridge

import (
	"context"
	"sort"
	"testing"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// fakeScopeKC serves canned groups and paged member lists so poller tests can
// drive the group-scoping path without HTTP.
type fakeScopeKC struct {
	users          []keycloak.User
	groups         []keycloak.Group
	members        map[string][]keycloak.User // groupID -> full member list
	listUsersCalls int
}

func (f *fakeScopeKC) ListUsers(ctx context.Context, params keycloak.ListUsersParams) ([]keycloak.User, error) {
	f.listUsersCalls++
	return f.users, nil
}

func (f *fakeScopeKC) ListGroups(ctx context.Context, search string) ([]keycloak.Group, error) {
	return f.groups, nil
}

func (f *fakeScopeKC) ListGroupMembers(ctx context.Context, groupID string, first, max int) ([]keycloak.User, int, error) {
	all := f.members[groupID]
	if first >= len(all) {
		return nil, 0, nil
	}
	end := first + max
	if end > len(all) {
		end = len(all)
	}
	page := all[first:end]
	return page, len(page), nil
}

func (f *fakeScopeKC) Realm() string { return "maximo" }

// recordingMAS records created usernames so tests can assert exactly which
// users made it through the scope.
type recordingMAS struct {
	created []string
}

func (r *recordingMAS) CreateUser(ctx context.Context, profileID string, payload mas.UserResource) (string, error) {
	r.created = append(r.created, payload.UserName)
	return "mas-" + payload.UserName, nil
}

func (r *recordingMAS) UpdateUser(ctx context.Context, profileID, masID string, payload mas.UserResource) error {
	return nil
}

func (r *recordingMAS) PatchUser(ctx context.Context, profileID, masID string, ops []mas.PatchOperation) error {
	return nil
}

func (r *recordingMAS) SearchUsers(ctx context.Context, profileID, filter string) ([]mas.User, error) {
	return nil, nil
}

func newTestPoller(t *testing.T, kc keycloakSource, masClient MASClient, includeUsernames []string, includePrefix string, includeGroups []string) *Poller {
	t.Helper()
	store, err := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	resolver := NewProfileResolver("p", nil, false)
	planner := NewPlanner(store, resolver, stubLogger{})
	executor := NewExecutor(masClient, store, false, true, stubLogger{})
	return NewPoller(kc, planner, executor, includeUsernames, includePrefix, includeGroups, stubLogger{})
}

func TestPollerGroupScopeSourcesFromMembership(t *testing.T) {
	kc := &fakeScopeKC{
		users:  []keycloak.User{{ID: "x1", Username: "other.user"}},
		groups: []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{
			"g1": {
				{ID: "u1", Username: "scim.user1"},
				{ID: "u2", Username: "scim.user2"},
			},
		},
	}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "", []string{"mas-scim-users"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if kc.listUsersCalls != 0 {
		t.Fatalf("expected ListUsers to be bypassed when groups configured, got %d calls", kc.listUsersCalls)
	}
	if len(masClient.created) != 2 {
		t.Fatalf("expected 2 creates from group membership, got %v", masClient.created)
	}
}

func TestPollerGroupScopeResolvesByPath(t *testing.T) {
	kc := &fakeScopeKC{
		groups: []keycloak.Group{
			{ID: "g0", Name: "users", Path: "/other/users"},
			{ID: "g1", Name: "users", Path: "/mas/users"},
		},
		members: map[string][]keycloak.User{
			"g1": {{ID: "u1", Username: "scim.user1"}},
		},
	}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "", []string{"/mas/users"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.created) != 1 || masClient.created[0] != "scim.user1" {
		t.Fatalf("expected path match to select group g1, got creates %v", masClient.created)
	}
}

func TestPollerGroupAndPrefixCompose(t *testing.T) {
	kc := &fakeScopeKC{
		groups: []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{
			"g1": {
				{ID: "u1", Username: "scim.user1"},
				{ID: "u2", Username: "oidc.user1"},
			},
		},
	}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "scim.", []string{"mas-scim-users"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.created) != 1 || masClient.created[0] != "scim.user1" {
		t.Fatalf("expected prefix to narrow group membership to scim.user1, got %v", masClient.created)
	}
}

func TestPollerUnresolvableGroupFailsCycle(t *testing.T) {
	kc := &fakeScopeKC{
		groups: []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{
			"g1": {{ID: "u1", Username: "scim.user1"}},
		},
	}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "", []string{"mas-scim-userz"})

	if err := p.RunOnce(context.Background()); err == nil {
		t.Fatalf("expected error for unresolvable group name")
	}
	if len(masClient.created) != 0 {
		t.Fatalf("expected no actions when a group cannot be resolved, got %v", masClient.created)
	}
}

func TestPollerDedupesAcrossOverlappingGroups(t *testing.T) {
	shared := keycloak.User{ID: "u1", Username: "scim.user1"}
	kc := &fakeScopeKC{
		groups: []keycloak.Group{
			{ID: "g1", Name: "group-a", Path: "/group-a"},
			{ID: "g2", Name: "group-b", Path: "/group-b"},
		},
		members: map[string][]keycloak.User{
			"g1": {shared, {ID: "u2", Username: "scim.user2"}},
			"g2": {shared, {ID: "u3", Username: "scim.user3"}},
		},
	}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "", []string{"group-a", "group-b"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	sort.Strings(masClient.created)
	want := []string{"scim.user1", "scim.user2", "scim.user3"}
	if len(masClient.created) != len(want) {
		t.Fatalf("expected union of both groups deduped, got %v", masClient.created)
	}
	for i, u := range want {
		if masClient.created[i] != u {
			t.Fatalf("expected creates %v, got %v", want, masClient.created)
		}
	}
}

func TestPollerGroupMembersPaginateBeyondSinglePage(t *testing.T) {
	members := make([]keycloak.User, 0, 120)
	for i := 0; i < 120; i++ {
		members = append(members, keycloak.User{
			ID:       "u" + string(rune('a'+i/26)) + string(rune('a'+i%26)),
			Username: "scim.user" + string(rune('a'+i/26)) + string(rune('a'+i%26)),
		})
	}
	kc := &fakeScopeKC{
		groups:  []keycloak.Group{{ID: "g1", Name: "big-group", Path: "/big-group"}},
		members: map[string][]keycloak.User{"g1": members},
	}
	masClient := &recordingMAS{}
	p := newTestPoller(t, kc, masClient, nil, "", []string{"big-group"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.created) != 120 {
		t.Fatalf("expected all 120 members across pages (no 50-user cap), got %d", len(masClient.created))
	}
}
