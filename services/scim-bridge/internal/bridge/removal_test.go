package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// patchCall records one PatchUser invocation so tests can assert exactly
// which MAS users were deactivated/reactivated and with what ops.
type patchCall struct {
	masID string
	ops   []mas.PatchOperation
}

// removalMAS records patch/update traffic and can fail patches on demand.
type removalMAS struct {
	patches  []patchCall
	patchErr error
	puts     []string
	created  []string
}

func (r *removalMAS) CreateUser(ctx context.Context, profileID string, payload mas.UserResource) (string, error) {
	r.created = append(r.created, payload.UserName)
	return "mas-" + payload.UserName, nil
}

func (r *removalMAS) UpdateUser(ctx context.Context, profileID, masID string, payload mas.UserResource) error {
	r.puts = append(r.puts, masID)
	return nil
}

func (r *removalMAS) PatchUser(ctx context.Context, profileID, masID string, ops []mas.PatchOperation) error {
	if r.patchErr != nil {
		return r.patchErr
	}
	r.patches = append(r.patches, patchCall{masID: masID, ops: ops})
	return nil
}

func (r *removalMAS) SearchUsers(ctx context.Context, profileID, filter string) ([]mas.User, error) {
	return nil, nil
}

// capturingLogger records warn lines so tests can assert the empty-scope
// mass-deactivation warning fires.
type capturingLogger struct {
	warns []string
}

func (c *capturingLogger) Info(string, ...any)          {}
func (c *capturingLogger) Warn(msg string, args ...any) { c.warns = append(c.warns, msg) }
func (c *capturingLogger) Error(string, ...any)         {}

func newRemovalTestPoller(t *testing.T, kc keycloakSource, masClient MASClient, includePrefix string, includeGroups []string, logger Logger) (*Poller, state.Store) {
	t.Helper()
	store, err := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	resolver := NewProfileResolver("p", nil, false)
	planner := NewPlanner(store, resolver, logger)
	executor := NewExecutor(masClient, store, false, true, logger)
	return NewPoller(kc, planner, executor, nil, includePrefix, includeGroups, logger), store
}

func deactivateOps(ops []mas.PatchOperation) bool {
	return len(ops) == 1 && ops[0].Op == "replace" && ops[0].Path == "active" && ops[0].Value == false
}

func TestPollerDeactivatesUserRemovedFromGroup(t *testing.T) {
	kc := &fakeScopeKC{
		groups: []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{
			"g1": {{ID: "u2", Username: "scim.user2", Enabled: true}},
		},
	}
	masClient := &removalMAS{}
	p, store := newRemovalTestPoller(t, kc, masClient, "", []string{"mas-scim-users"}, stubLogger{})
	_ = store.Save(context.Background(), "u1", state.Entry{MASID: "mas-u1", ProfileID: "p", Status: state.StatusOK, Username: "scim.user1"})
	// Failed create: never materialized in MAS, must not plan a deactivation.
	_ = store.Save(context.Background(), "u3", state.Entry{MASID: "", ProfileID: "p", Status: state.StatusError, LastError: "boom", Username: "scim.user3"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.patches) != 1 || masClient.patches[0].masID != "mas-u1" || !deactivateOps(masClient.patches[0].ops) {
		t.Fatalf("expected single active:false patch for mas-u1, got %+v", masClient.patches)
	}
	entry, ok, _ := store.Lookup(context.Background(), "u1")
	if !ok || entry.Status != state.StatusDeactivated || entry.MASID != "mas-u1" {
		t.Fatalf("expected tombstoned entry for u1, got %+v ok=%v", entry, ok)
	}
	entry3, _, _ := store.Lookup(context.Background(), "u3")
	if entry3.Status != state.StatusError {
		t.Fatalf("expected u3 (no MAS ID) untouched, got %+v", entry3)
	}
}

func TestPollerTombstonedUserSkippedOnLaterCycles(t *testing.T) {
	kc := &fakeScopeKC{
		groups:  []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{"g1": {}},
	}
	masClient := &removalMAS{}
	p, store := newRemovalTestPoller(t, kc, masClient, "", []string{"mas-scim-users"}, stubLogger{})
	_ = store.Save(context.Background(), "u1", state.Entry{MASID: "mas-u1", ProfileID: "p", Status: state.StatusDeactivated, Username: "scim.user1"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.patches) != 0 {
		t.Fatalf("expected no MAS calls for tombstoned user, got %+v", masClient.patches)
	}
}

func TestPollerReAddReactivatesAndClearsTombstone(t *testing.T) {
	kc := &fakeScopeKC{
		groups: []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{
			"g1": {{ID: "u1", Username: "scim.user1", Enabled: true}},
		},
	}
	masClient := &removalMAS{}
	p, store := newRemovalTestPoller(t, kc, masClient, "", []string{"mas-scim-users"}, stubLogger{})
	_ = store.Save(context.Background(), "u1", state.Entry{
		MASID: "mas-u1", ProfileID: "p", Status: state.StatusDeactivated, Username: "scim.user1",
		LastApplied: state.Snapshot{UserName: "scim.user1", HasActive: true, Active: false, ProfileID: "p"},
	})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.patches) != 1 || masClient.patches[0].masID != "mas-u1" {
		t.Fatalf("expected one reactivation patch, got %+v", masClient.patches)
	}
	ops := masClient.patches[0].ops
	found := false
	for _, op := range ops {
		if op.Op == "replace" && op.Path == "active" && op.Value == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected replace active:true op, got %+v", ops)
	}
	entry, ok, _ := store.Lookup(context.Background(), "u1")
	if !ok || entry.Status != state.StatusOK {
		t.Fatalf("expected tombstone cleared to ok, got %+v ok=%v", entry, ok)
	}
	if !entry.LastApplied.Active {
		t.Fatalf("expected snapshot active after reactivation, got %+v", entry.LastApplied)
	}
}

func TestPollerDeactivateFailureRetriesNextCycle(t *testing.T) {
	kc := &fakeScopeKC{
		groups:  []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{"g1": {}},
	}
	masClient := &removalMAS{patchErr: &mas.ResponseError{StatusCode: 500, Status: "500 Internal Server Error", Body: "boom"}}
	p, store := newRemovalTestPoller(t, kc, masClient, "", []string{"mas-scim-users"}, stubLogger{})
	_ = store.Save(context.Background(), "u1", state.Entry{MASID: "mas-u1", ProfileID: "p", Status: state.StatusOK, Username: "scim.user1"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	entry, _, _ := store.Lookup(context.Background(), "u1")
	if entry.Status == state.StatusDeactivated {
		t.Fatalf("failed deactivation must not tombstone, got %+v", entry)
	}
	if entry.Status != state.StatusError || entry.MASID != "mas-u1" {
		t.Fatalf("expected error state retaining MAS ID for retry, got %+v", entry)
	}

	masClient.patchErr = nil
	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce retry: %v", err)
	}
	if len(masClient.patches) != 1 || masClient.patches[0].masID != "mas-u1" || !deactivateOps(masClient.patches[0].ops) {
		t.Fatalf("expected deactivation retried, got %+v", masClient.patches)
	}
	entry, _, _ = store.Lookup(context.Background(), "u1")
	if entry.Status != state.StatusDeactivated {
		t.Fatalf("expected tombstone after successful retry, got %+v", entry)
	}
}

func TestPollerKeycloakErrorProducesZeroDeactivations(t *testing.T) {
	kc := &fakeScopeKC{
		groups:     []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		membersErr: errors.New("keycloak 502"),
	}
	masClient := &removalMAS{}
	p, store := newRemovalTestPoller(t, kc, masClient, "", []string{"mas-scim-users"}, stubLogger{})
	_ = store.Save(context.Background(), "u1", state.Entry{MASID: "mas-u1", ProfileID: "p", Status: state.StatusOK, Username: "scim.user1"})

	if err := p.RunOnce(context.Background()); err == nil {
		t.Fatalf("expected cycle error when Keycloak member listing fails")
	}
	if len(masClient.patches) != 0 {
		t.Fatalf("expected zero deactivations on Keycloak failure, got %+v", masClient.patches)
	}
	entry, _, _ := store.Lookup(context.Background(), "u1")
	if entry.Status != state.StatusOK {
		t.Fatalf("expected state untouched on Keycloak failure, got %+v", entry)
	}
}

func TestPollerPrefixModeNeverDeactivates(t *testing.T) {
	// Legacy ListUsers mode returns a single page; u1 is tracked in state but
	// absent from the page, and must NOT be deactivated.
	kc := &fakeScopeKC{
		users: []keycloak.User{{ID: "u2", Username: "scim.user2", Enabled: true}},
	}
	masClient := &removalMAS{}
	p, store := newRemovalTestPoller(t, kc, masClient, "scim.", nil, stubLogger{})
	_ = store.Save(context.Background(), "u1", state.Entry{MASID: "mas-u1", ProfileID: "p", Status: state.StatusOK, Username: "scim.user1"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	for _, call := range masClient.patches {
		if deactivateOps(call.ops) {
			t.Fatalf("prefix mode must not deactivate, got %+v", masClient.patches)
		}
	}
	entry, _, _ := store.Lookup(context.Background(), "u1")
	if entry.Status != state.StatusOK {
		t.Fatalf("expected u1 untouched in prefix mode, got %+v", entry)
	}
}

func TestPollerEmptyGroupDeactivatesAllWithWarning(t *testing.T) {
	kc := &fakeScopeKC{
		groups:  []keycloak.Group{{ID: "g1", Name: "mas-scim-users", Path: "/mas-scim-users"}},
		members: map[string][]keycloak.User{"g1": {}},
	}
	masClient := &removalMAS{}
	logger := &capturingLogger{}
	p, store := newRemovalTestPoller(t, kc, masClient, "", []string{"mas-scim-users"}, logger)
	_ = store.Save(context.Background(), "u1", state.Entry{MASID: "mas-u1", ProfileID: "p", Status: state.StatusOK, Username: "scim.user1"})
	_ = store.Save(context.Background(), "u2", state.Entry{MASID: "mas-u2", ProfileID: "p", Status: state.StatusOK, Username: "scim.user2"})

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(masClient.patches) != 2 {
		t.Fatalf("expected both tracked users deactivated on legitimately empty group, got %+v", masClient.patches)
	}
	warned := false
	for _, w := range logger.warns {
		if strings.Contains(w, "scoped set is empty") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected empty-scope warning before mass deactivation, got warns %v", logger.warns)
	}
}
