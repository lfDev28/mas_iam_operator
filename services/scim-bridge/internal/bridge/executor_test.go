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

type fakeMAS struct {
	createID      string
	createErr     error
	updateErr     error
	updateCalled  int
	lastPutID     string
	lastPut       mas.UserResource
	patchErr      error
	patchOps      []mas.PatchOperation
	patchID       string
	patchProfile  string
	patchCalls    int
	searchErrs    []error
	searchResults [][]mas.User
	searchErr     error
	searchResult  []mas.User
	lastFilter    string
	searchCalls   int
}

func (f *fakeMAS) CreateUser(ctx context.Context, profileID string, payload mas.UserResource) (string, error) {
	return f.createID, f.createErr
}

func (f *fakeMAS) UpdateUser(ctx context.Context, profileID, masID string, payload mas.UserResource) error {
	f.updateCalled++
	f.lastPutID = masID
	f.lastPut = payload
	return f.updateErr
}

func (f *fakeMAS) SearchUsers(ctx context.Context, profileID, filter string) ([]mas.User, error) {
	f.lastFilter = filter
	if f.searchCalls < len(f.searchResults) {
		res := f.searchResults[f.searchCalls]
		var err error
		if f.searchCalls < len(f.searchErrs) {
			err = f.searchErrs[f.searchCalls]
		}
		f.searchCalls++
		return res, err
	}
	return f.searchResult, f.searchErr
}

func (f *fakeMAS) PatchUser(ctx context.Context, profileID, masID string, ops []mas.PatchOperation) error {
	f.patchCalls++
	f.patchProfile = profileID
	f.patchID = masID
	f.patchOps = ops
	return f.patchErr
}

func TestExecutorCreateStoresID(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	exec := NewExecutor(&fakeMAS{createID: "mas-1"}, store, false, true, stubLogger{})

	action := Action{
		Type:         ActionCreate,
		User:         keycloak.User{ID: "kc1", Username: "alice"},
		ProfileID:    "p",
		ProfileLabel: "users",
	}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.MASID != "mas-1" || entry.ProfileID != "p" || entry.Status != state.StatusOK {
		t.Fatalf("unexpected entry: %+v (ok=%v)", entry, ok)
	}
}

func TestExecutorConflictAdoptsExisting(t *testing.T) {
	conflict := &mas.ResponseError{StatusCode: 409, Status: "409 Conflict", Body: "conflict"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{
		createErr:    conflict,
		searchResult: []mas.User{{ID: "adopt-1", ExternalID: "kc1", UserName: "alice"}},
	}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{
		Type:         ActionCreate,
		User:         keycloak.User{ID: "kc1", Username: "alice", Email: "a@example.com", Enabled: true},
		ProfileID:    "p",
		ProfileLabel: "users",
	}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.lastFilter != `externalId eq "kc1"` {
		t.Fatalf("expected externalId filter, got %q", fake.lastFilter)
	}
	// Adoption must push the desired payload, not just record it.
	if fake.updateCalled != 1 || fake.lastPutID != "adopt-1" || fake.lastPut.UserName != "alice" {
		t.Fatalf("expected PUT of desired payload to adopted user, got calls=%d id=%q payload=%+v", fake.updateCalled, fake.lastPutID, fake.lastPut)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.MASID != "adopt-1" || entry.Status != state.StatusOK || !entry.CreatedByBridge || entry.LastApplied.Email != "a@example.com" {
		t.Fatalf("unexpected entry after adopt: %+v (ok=%v)", entry, ok)
	}
}

func TestExecutorConflictAdoptFailedPutRetriesAsUpdate(t *testing.T) {
	conflict := &mas.ResponseError{StatusCode: 409, Status: "409 Conflict", Body: "conflict"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{
		createErr:    conflict,
		updateErr:    &mas.ResponseError{StatusCode: 503, Status: "503 Service Unavailable"},
		searchResult: []mas.User{{ID: "adopt-1", ExternalID: "kc1"}},
	}
	exec := NewExecutor(fake, store, false, true, stubLogger{})
	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	entry, _, _ := store.Lookup(context.Background(), "kc1")
	if entry.MASID != "adopt-1" || entry.Status != state.StatusError || entry.ErrorClass != state.ErrorClassTransient || !entry.LastApplied.IsZero() {
		t.Fatalf("expected adopted MAS ID kept with transient error and no snapshot, got %+v", entry)
	}
}

func TestExecutorConflictZeroResultsMarksError(t *testing.T) {
	conflict := &mas.ResponseError{StatusCode: 409, Status: "409 Conflict", Body: "conflict"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{createErr: conflict, searchResult: []mas.User{}}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	_ = exec.Execute(context.Background(), action)

	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.Status != state.StatusError || entry.MASID != "" {
		t.Fatalf("expected error entry after zero results, got %+v (ok=%v)", entry, ok)
	}
}

func TestExecutorConflictMultipleResultsMarksError(t *testing.T) {
	conflict := &mas.ResponseError{StatusCode: 409, Status: "409 Conflict", Body: "conflict"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{createErr: conflict, searchResult: []mas.User{{ID: "a"}, {ID: "b"}}}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	_ = exec.Execute(context.Background(), action)

	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.Status != state.StatusError {
		t.Fatalf("expected error entry after multiple results, got %+v (ok=%v)", entry, ok)
	}
}

func TestExecutorConflictFallbackToUsernameFilterAdoptsWhenExternalIDMatches(t *testing.T) {
	conflict := &mas.ResponseError{StatusCode: 409, Status: "409 Conflict", Body: "conflict"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{
		createErr:     conflict,
		searchErrs:    []error{errors.New("externalId unsupported")},
		searchResults: [][]mas.User{{}, {{ID: "adopt-username", UserName: "alice", ExternalID: "kc1"}}},
	}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	_ = exec.Execute(context.Background(), action)

	if fake.lastFilter != `userName eq "alice"` {
		t.Fatalf("expected username fallback filter, got %q", fake.lastFilter)
	}
	if fake.updateCalled != 1 || fake.lastPutID != "adopt-username" {
		t.Fatalf("expected desired payload PUT after adoption, got calls=%d id=%q", fake.updateCalled, fake.lastPutID)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.MASID != "adopt-username" || entry.Status != state.StatusOK || entry.LastApplied.IsZero() {
		t.Fatalf("unexpected entry after fallback adopt: %+v (ok=%v)", entry, ok)
	}
}

func TestExecutorConflictRefusesToAdoptUnownedUser(t *testing.T) {
	conflict := &mas.ResponseError{StatusCode: 409, Status: "409 Conflict", Body: "conflict"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{
		createErr: conflict,
		// externalId search: nothing; userName search: a pre-existing
		// MAS user with no (or someone else's) externalId.
		searchResults: [][]mas.User{{}, {{ID: "someone-else", UserName: "alice", ExternalID: "other-system-42"}}},
		searchErrs:    []error{nil, nil},
	}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.updateCalled != 0 || fake.patchCalls != 0 {
		t.Fatalf("must not write to an unowned MAS user, got put=%d patch=%d", fake.updateCalled, fake.patchCalls)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.MASID != "" || entry.Status != state.StatusError || entry.ErrorClass != state.ErrorClassConflict || !entry.Parked {
		t.Fatalf("expected parked conflict with no MAS ID, got %+v (ok=%v)", entry, ok)
	}
	if !strings.Contains(entry.LastError, "not owned by this bridge") {
		t.Fatalf("expected clear conflict message, got %q", entry.LastError)
	}
}

func TestExecutorCreatePersistentErrorMarksState(t *testing.T) {
	respErr := &mas.ResponseError{StatusCode: 500, Status: "500 Internal Server Error", Body: "boom"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{createErr: respErr}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	_ = exec.Execute(context.Background(), action)
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.Status != state.StatusError {
		t.Fatalf("expected error state for persistent create failure, got %+v ok=%v", entry, ok)
	}
}

func TestExecutorCreateUnauthorizedReturnsRetryableError(t *testing.T) {
	respErr := &mas.ResponseError{StatusCode: 401, Status: "401 Unauthorized", Body: "expired"}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	fake := &fakeMAS{createErr: respErr}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionCreate, User: keycloak.User{ID: "kc1", Username: "alice"}, ProfileID: "p"}
	err := exec.Execute(context.Background(), action)
	if !errors.Is(err, ErrMASUnauthorized) {
		t.Fatalf("expected ErrMASUnauthorized, got %v", err)
	}
	if _, ok, _ := store.Lookup(context.Background(), "kc1"); ok {
		t.Fatalf("did not expect state entry on unauthorized create")
	}
}

func TestExecutorUpdateUsesPatchForChanges(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = store.Save(context.Background(), "kc1", state.Entry{
		MASID: "mas1", ProfileID: "p", Status: state.StatusOK, Username: "alice",
		LastApplied: state.Snapshot{
			UserName:   "alice",
			HasActive:  true,
			Active:     true,
			HasEmail:   true,
			Email:      "a@example.com",
			GivenName:  "Alice",
			FamilyName: "Smith",
			ProfileID:  "p",
		},
	})
	fake := &fakeMAS{}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{
		Type:             ActionUpdate,
		User:             keycloak.User{ID: "kc1", Username: "alice", FirstName: "Alice", LastName: "Smith", Email: "b@example.com", Enabled: false},
		ExistingMAS:      "mas1",
		ProfileID:        "p",
		StateLastApplied: state.Snapshot{UserName: "alice", HasActive: true, Active: true, HasEmail: true, Email: "a@example.com", ProfileID: "p", GivenName: "Alice", FamilyName: "Smith"},
	}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.patchCalls != 1 || fake.patchID != "mas1" || fake.patchProfile != "p" {
		t.Fatalf("expected patch call once, got calls=%d id=%s profile=%s", fake.patchCalls, fake.patchID, fake.patchProfile)
	}
	if len(fake.patchOps) < 2 {
		t.Fatalf("expected patch ops, got %#v", fake.patchOps)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.LastApplied.Email != "b@example.com" || entry.LastApplied.Active {
		t.Fatalf("snapshot not updated after patch: %+v", entry.LastApplied)
	}
}

func TestExecutorUpdateNoChangesSkipsPatch(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	snap := state.Snapshot{UserName: "alice", HasActive: true, Active: true, HasEmail: true, Email: "a@example.com", ProfileID: "p"}
	_ = store.Save(context.Background(), "kc1", state.Entry{
		MASID: "mas1", ProfileID: "p", Status: state.StatusOK, Username: "alice", LastApplied: snap,
	})
	fake := &fakeMAS{}
	exec := NewExecutor(fake, store, false, true, stubLogger{})
	action := Action{Type: ActionUpdate, User: keycloak.User{ID: "kc1", Username: "alice", Email: "a@example.com", Enabled: true}, ExistingMAS: "mas1", ProfileID: "p", StateLastApplied: snap}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.patchCalls != 0 {
		t.Fatalf("expected no patch call for no-op update")
	}
}

func TestExecutorUpdateFallsBackToPutWhenNoSnapshot(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = store.Save(context.Background(), "kc1", state.Entry{MASID: "mas1", ProfileID: "p", Status: state.StatusOK, Username: "alice"})
	fake := &fakeMAS{}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionUpdate, User: keycloak.User{ID: "kc1", Username: "alice"}, ExistingMAS: "mas1", ProfileID: "p"}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.updateCalled != 1 {
		t.Fatalf("expected PUT fallback, got updateCalled=%d", fake.updateCalled)
	}
}

func TestExecutorUpdatePatch404FallsBackToPut(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	snap := state.Snapshot{UserName: "alice", HasActive: true, Active: true, ProfileID: "p"}
	_ = store.Save(context.Background(), "kc1", state.Entry{MASID: "mas1", ProfileID: "p", Status: state.StatusOK, Username: "alice", LastApplied: snap})
	fake := &fakeMAS{patchErr: &mas.ResponseError{StatusCode: 404, Status: "404 Not Found", Body: "not found"}}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{Type: ActionUpdate, User: keycloak.User{ID: "kc1", Username: "alice"}, ExistingMAS: "mas1", ProfileID: "p", StateLastApplied: snap}
	if err := exec.Execute(context.Background(), action); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fake.patchCalls != 1 || fake.updateCalled != 1 {
		t.Fatalf("expected patch then put fallback, got patch=%d put=%d", fake.patchCalls, fake.updateCalled)
	}
}

func TestExecutorUpdateUnauthorizedReturnsRetryableError(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	snap := state.Snapshot{UserName: "alice", HasActive: true, Active: true, ProfileID: "p"}
	_ = store.Save(context.Background(), "kc1", state.Entry{
		MASID: "mas1", ProfileID: "p", Status: state.StatusOK, Username: "alice", LastApplied: snap,
	})
	fake := &fakeMAS{patchErr: &mas.ResponseError{StatusCode: 401, Status: "401 Unauthorized", Body: "expired"}}
	exec := NewExecutor(fake, store, false, true, stubLogger{})

	action := Action{
		Type:             ActionUpdate,
		User:             keycloak.User{ID: "kc1", Username: "alice", Enabled: false},
		ExistingMAS:      "mas1",
		ProfileID:        "p",
		StateLastApplied: snap,
	}
	err := exec.Execute(context.Background(), action)
	if !errors.Is(err, ErrMASUnauthorized) {
		t.Fatalf("expected ErrMASUnauthorized, got %v", err)
	}
	entry, ok, _ := store.Lookup(context.Background(), "kc1")
	if !ok || entry.Status != state.StatusOK {
		t.Fatalf("expected state unchanged on unauthorized update, got %+v ok=%v", entry, ok)
	}
}
