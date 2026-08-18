package bridge

import (
	"context"
	"testing"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

type fakeSearchMAS struct {
	results map[string]string
	err     error
	calls   []string
}

func (f *fakeSearchMAS) CreateUser(ctx context.Context, profileID string, payload mas.UserResource) (string, error) {
	return "", nil
}

func (f *fakeSearchMAS) UpdateUser(ctx context.Context, profileID, masID string, payload mas.UserResource) error {
	return nil
}

func (f *fakeSearchMAS) PatchUser(ctx context.Context, profileID, masID string, ops []mas.PatchOperation) error {
	return nil
}

func (f *fakeSearchMAS) SearchUsers(ctx context.Context, profileID, filter string) ([]mas.User, error) {
	f.calls = append(f.calls, profileID+":"+filter)
	if f.err != nil {
		return nil, f.err
	}
	if id, ok := f.results[filter]; ok {
		return []mas.User{{ID: id}}, nil
	}
	return []mas.User{}, nil
}

type fakeKC struct {
	users []keycloak.User
	err   error
}

func (f *fakeKC) ListUsers(ctx context.Context, params keycloak.ListUsersParams) ([]keycloak.User, error) {
	return f.users, f.err
}

func (f *fakeKC) ListGroups(ctx context.Context, search string) ([]keycloak.Group, error) {
	return nil, nil
}

func (f *fakeKC) ListGroupMembers(ctx context.Context, groupID string, first, max int) ([]keycloak.User, int, error) {
	return nil, 0, nil
}

func (f *fakeKC) Realm() string { return "maximo" }

func TestBackfillMapsSingleUser(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	resolver := NewProfileResolver("p", map[string]string{"users": "p"}, false)
	b := NewBackfill(&fakeKC{
		users: []keycloak.User{{ID: "kc1", Username: "alice", MasProfile: "users"}},
	}, resolver, store, nil, "", nil, stubLogger{}).WithMASClient(&fakeSearchMAS{
		results: map[string]string{`externalId eq "kc1"`: "mas1"},
	})
	if err := b.Run(context.Background()); err != nil {
		t.Fatalf("backfill run: %v", err)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.MASID != "mas1" || entry.ProfileID != "p" || entry.Status != state.StatusOK {
		t.Fatalf("unexpected entry: %+v ok=%v", entry, ok)
	}
}

func TestBackfillMarksErrorWhenMissing(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	resolver := NewProfileResolver("p", map[string]string{"users": "p"}, false)
	b := NewBackfill(&fakeKC{
		users: []keycloak.User{{ID: "kc1", Username: "alice", MasProfile: "users"}},
	}, resolver, store, nil, "", nil, stubLogger{}).WithMASClient(&fakeSearchMAS{})

	_ = b.Run(context.Background())
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.Status != state.StatusError {
		t.Fatalf("expected error entry when no MAS user found, got %+v ok=%v", entry, ok)
	}
}
