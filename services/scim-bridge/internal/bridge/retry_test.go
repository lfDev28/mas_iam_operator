package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// scriptedMAS fails CreateUser/PatchUser/UpdateUser with the configured
// errors and records every call.
type scriptedMAS struct {
	createErr error
	patchErr  error
	putErr    error
	creates   []mas.UserResource
	patches   []patchCall
	puts      []string
}

func (s *scriptedMAS) CreateUser(ctx context.Context, profileID string, payload mas.UserResource) (string, error) {
	s.creates = append(s.creates, payload)
	if s.createErr != nil {
		return "", s.createErr
	}
	return "mas-" + payload.UserName, nil
}

func (s *scriptedMAS) UpdateUser(ctx context.Context, profileID, masID string, payload mas.UserResource) error {
	s.puts = append(s.puts, masID)
	return s.putErr
}

func (s *scriptedMAS) PatchUser(ctx context.Context, profileID, masID string, ops []mas.PatchOperation) error {
	s.patches = append(s.patches, patchCall{masID: masID, ops: ops})
	return s.patchErr
}

func (s *scriptedMAS) SearchUsers(ctx context.Context, profileID, filter string) ([]mas.User, error) {
	return nil, nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

type retryHarness struct {
	kc     *fakeScopeKC
	mas    *scriptedMAS
	store  state.Store
	clock  *fakeClock
	poller *Poller
	exec   *Executor
}

func newRetryHarness(t *testing.T, users []keycloak.User, store state.Store, dryRun bool) *retryHarness {
	t.Helper()
	if store == nil {
		var err error
		store, err = state.NewStore("memory", state.Options{DefaultProfileID: "p"})
		if err != nil {
			t.Fatalf("new store: %v", err)
		}
	}
	h := &retryHarness{
		kc:    &fakeScopeKC{users: users},
		mas:   &scriptedMAS{},
		store: store,
		clock: &fakeClock{t: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)},
	}
	resolver := NewProfileResolver("p", nil, false)
	h.exec = NewExecutor(h.mas, store, dryRun, true, stubLogger{}).WithClock(h.clock.now).WithConfigFingerprint("cfg-a")
	h.poller = NewPoller(h.kc, NewPlanner(store, resolver, stubLogger{}), h.exec, nil, "", nil, stubLogger{})
	return h
}

func (h *retryHarness) cycle(t *testing.T) {
	t.Helper()
	if err := h.poller.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	h.clock.advance(5 * time.Minute)
}

func (h *retryHarness) entry(t *testing.T, id string) state.Entry {
	t.Helper()
	e, ok, _ := h.store.Lookup(context.Background(), id)
	if !ok {
		t.Fatalf("no state entry for %s", id)
	}
	return e
}

var mas500 = &mas.ResponseError{StatusCode: 500, Status: "500 Internal Server Error", Body: ""}

// The sticky-error regression: MAS returns 500 (fresh-MAS AIUI1101E with
// an empty body) for N cycles, then recovers; the user must get created.
func TestStickyTransientCreateFailureRecoversWhenMASRecovers(t *testing.T) {
	user := keycloak.User{ID: "u1", Username: "scim.user1", Email: "u1@example.com", Enabled: true}
	h := newRetryHarness(t, []keycloak.User{user}, nil, false)

	h.mas.createErr = mas500
	const failingCycles = 4
	for i := 0; i < failingCycles; i++ {
		h.cycle(t)
	}
	e := h.entry(t, "u1")
	if e.Status != state.StatusError || e.ErrorClass != state.ErrorClassTransient || e.Parked {
		t.Fatalf("expected retryable transient error, got %+v", e)
	}
	if e.Attempts != failingCycles {
		t.Fatalf("expected %d attempts recorded, got %d", failingCycles, e.Attempts)
	}

	h.mas.createErr = nil
	// Backoff may defer the first post-recovery attempt; it must land
	// within the 30-minute cap.
	for i := 0; i < 7 && h.entry(t, "u1").Status != state.StatusOK; i++ {
		h.cycle(t)
	}
	e = h.entry(t, "u1")
	if e.Status != state.StatusOK || e.MASID != "mas-scim.user1" || !e.CreatedByBridge {
		t.Fatalf("expected user created after MAS recovered, got %+v", e)
	}
	if e.Attempts != 0 || e.ErrorClass != "" || !e.NextRetryAt.IsZero() || e.LastError != "" {
		t.Fatalf("expected retry bookkeeping cleared on success, got %+v", e)
	}
	if got := len(h.mas.creates); got != failingCycles+1 {
		t.Fatalf("expected %d create attempts, got %d", failingCycles+1, got)
	}
}

func TestBackoffDefersRetriesAndCapsAtThirtyMinutes(t *testing.T) {
	p := DefaultRetryPolicy()
	if got := p.backoff(1); got != 30*time.Second {
		t.Fatalf("backoff(1) = %s", got)
	}
	if got := p.backoff(3); got != 2*time.Minute {
		t.Fatalf("backoff(3) = %s", got)
	}
	if got := p.backoff(50); got != 30*time.Minute {
		t.Fatalf("backoff(50) = %s, want 30m cap", got)
	}

	user := keycloak.User{ID: "u1", Username: "scim.user1", Enabled: true}
	h := newRetryHarness(t, []keycloak.User{user}, nil, false)
	h.mas.createErr = mas500
	h.cycle(t) // attempt 1, retry due in 30s; the 5m cycle clears it
	h.clock.advance(-5 * time.Minute)
	if err := h.poller.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.mas.creates) != 1 {
		t.Fatalf("expected retry deferred inside backoff window, got %d creates", len(h.mas.creates))
	}
}

// An entry written by a pre-v0.1.16 bridge (status=error, no MAS ID, none of
// the retry fields) must load and be retried as a create.
func TestLegacyErrorEntryWithoutMASIDIsRetriedAsCreate(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = store.Save(context.Background(), "u1", state.Entry{Status: state.StatusError, LastError: "500 Internal Server Error: ", ProfileID: "p", Username: "scim.user1"})
	h := newRetryHarness(t, []keycloak.User{{ID: "u1", Username: "scim.user1", Enabled: true}}, store, false)
	h.cycle(t)
	if len(h.mas.creates) != 1 {
		t.Fatalf("expected legacy error entry retried as create, got %d creates", len(h.mas.creates))
	}
	if e := h.entry(t, "u1"); e.Status != state.StatusOK || e.MASID != "mas-scim.user1" {
		t.Fatalf("unexpected entry %+v", e)
	}
}

// A legacy error entry for an existing MAS user must not block updates: a
// user disabled in Keycloak has to be deactivated in MAS.
func TestLegacyErrorEntryStillPropagatesDisable(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = store.Save(context.Background(), "u1", state.Entry{
		MASID: "mas-u1", Status: state.StatusError, LastError: "503", ProfileID: "p", Username: "scim.user1",
		LastApplied: state.Snapshot{UserName: "scim.user1", HasActive: true, Active: true, ProfileID: "p"},
	})
	h := newRetryHarness(t, []keycloak.User{{ID: "u1", Username: "scim.user1", Enabled: false}}, store, false)
	h.cycle(t)
	if len(h.mas.patches) != 1 || !deactivateOps(h.mas.patches[0].ops) {
		t.Fatalf("expected active:false patch for disabled user, got %+v", h.mas.patches)
	}
	if e := h.entry(t, "u1"); e.Status != state.StatusOK || e.LastApplied.Active {
		t.Fatalf("unexpected entry %+v", e)
	}
}

// A user disabled in Keycloak while its entry is backing off must not wait
// out the backoff: the desired-state change bypasses it.
func TestDesiredStateChangeBypassesBackoff(t *testing.T) {
	user := keycloak.User{ID: "u1", Username: "scim.user1", Enabled: true}
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = store.Save(context.Background(), "u1", state.Entry{
		MASID: "mas-u1", Status: state.StatusOK, ProfileID: "p", Username: "scim.user1",
		LastApplied: state.Snapshot{UserName: "scim.user1", HasActive: true, Active: false, ProfileID: "p"},
	})
	h := newRetryHarness(t, []keycloak.User{user}, store, false)
	h.exec.WithRetryPolicy(RetryPolicy{BaseBackoff: time.Hour, MaxBackoff: time.Hour, ParkAfter: 3})
	h.mas.patchErr = mas500
	h.cycle(t) // reactivation patch fails; retry due in an hour
	if len(h.mas.patches) != 1 {
		t.Fatalf("expected one patch attempt, got %d", len(h.mas.patches))
	}
	h.cycle(t) // still backing off
	if len(h.mas.patches) != 1 {
		t.Fatalf("expected backoff to hold, got %d patches", len(h.mas.patches))
	}
	h.kc.users[0].Enabled = false
	h.mas.patchErr = nil
	h.cycle(t)
	e := h.entry(t, "u1")
	if e.Status != state.StatusOK || e.LastApplied.Active {
		t.Fatalf("expected disable applied despite backoff, got %+v", e)
	}
}

// Repeated non-retryable 4xx parks the entry; it stays parked until the
// desired payload or the bridge config changes.
func TestPermanentFailureParksThenUnparksOnChange(t *testing.T) {
	user := keycloak.User{ID: "u1", Username: "scim.user1", Email: "bad", Enabled: true}
	h := newRetryHarness(t, []keycloak.User{user}, nil, false)
	h.mas.createErr = &mas.ResponseError{StatusCode: 400, Status: "400 Bad Request", Body: "invalid email"}

	for i := 0; i < 3; i++ {
		h.cycle(t)
		h.clock.advance(time.Hour) // never let backoff be the reason we skip
	}
	e := h.entry(t, "u1")
	if !e.Parked || e.ErrorClass != state.ErrorClassPermanent || e.Attempts != 3 {
		t.Fatalf("expected parked after 3 permanent failures, got %+v", e)
	}
	for i := 0; i < 3; i++ {
		h.cycle(t)
		h.clock.advance(time.Hour)
	}
	if len(h.mas.creates) != 3 {
		t.Fatalf("expected no attempts while parked, got %d creates", len(h.mas.creates))
	}

	// Config fix: a new fingerprint re-attempts the parked entry.
	h.exec.WithConfigFingerprint("cfg-b")
	h.cycle(t)
	if len(h.mas.creates) != 4 {
		t.Fatalf("expected config change to re-attempt parked entry, got %d creates", len(h.mas.creates))
	}
	if e := h.entry(t, "u1"); e.Parked || e.Attempts != 1 {
		t.Fatalf("expected attempt counter reset for new desired state, got %+v", e)
	}

	// Payload fix in Keycloak: re-attempted and succeeds.
	h.mas.createErr = nil
	h.kc.users[0].Email = "u1@example.com"
	h.cycle(t)
	if e := h.entry(t, "u1"); e.Status != state.StatusOK || e.MASID == "" {
		t.Fatalf("expected create after payload change, got %+v", e)
	}
}

// Transient failures never park, however many there are.
func TestTransientFailuresNeverPark(t *testing.T) {
	h := newRetryHarness(t, []keycloak.User{{ID: "u1", Username: "scim.user1", Enabled: true}}, nil, false)
	h.mas.createErr = &mas.ResponseError{StatusCode: 429, Status: "429 Too Many Requests"}
	for i := 0; i < 12; i++ {
		h.cycle(t)
		h.clock.advance(time.Hour)
	}
	e := h.entry(t, "u1")
	if e.Parked || e.ErrorClass != state.ErrorClassTransient || e.Attempts != 12 {
		t.Fatalf("expected unparked transient entry with 12 attempts, got %+v", e)
	}
}

func TestDryRunPersistsNothingAndLiveRunCreates(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	user := keycloak.User{ID: "u1", Username: "scim.user1", Enabled: true}
	dry := newRetryHarness(t, []keycloak.User{user}, store, true)
	dry.cycle(t)
	if len(dry.mas.creates) != 0 {
		t.Fatalf("dry-run must not call MAS, got %d creates", len(dry.mas.creates))
	}
	if e, ok, _ := store.Lookup(context.Background(), "u1"); ok {
		t.Fatalf("dry-run must not persist state, got %+v", e)
	}

	live := newRetryHarness(t, []keycloak.User{user}, store, false)
	live.cycle(t)
	if len(live.mas.creates) != 1 {
		t.Fatalf("expected live run to create after dry-run, got %d", len(live.mas.creates))
	}
}

// State written by pre-v0.1.16 dry-runs holds fabricated dryrun-<id> MAS
// IDs with status ok; those users must be created, never patched.
func TestLegacyDryRunIDIsTreatedAsNotCreated(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = store.Save(context.Background(), "u1", state.Entry{
		MASID: "dryrun-u1", Status: state.StatusOK, ProfileID: "p", Username: "scim.user1",
		LastApplied: state.Snapshot{UserName: "scim.user1", HasActive: true, Active: true, ProfileID: "p"},
	})
	h := newRetryHarness(t, []keycloak.User{{ID: "u1", Username: "scim.user1", Enabled: true}}, store, false)
	h.cycle(t)
	if len(h.mas.creates) != 1 || len(h.mas.patches) != 0 || len(h.mas.puts) != 0 {
		t.Fatalf("expected a create and no updates, got creates=%d patches=%d puts=%d", len(h.mas.creates), len(h.mas.patches), len(h.mas.puts))
	}
	if e := h.entry(t, "u1"); e.MASID != "mas-scim.user1" {
		t.Fatalf("expected real MAS ID, got %+v", e)
	}
}

func TestReadOnlyStoreDiscardsSaves(t *testing.T) {
	inner, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p"})
	_ = inner.Save(context.Background(), "u1", state.Entry{MASID: "m1"})
	ro := state.ReadOnly(inner)
	if err := ro.Save(context.Background(), "u2", state.Entry{MASID: "m2"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := inner.Lookup(context.Background(), "u2"); ok {
		t.Fatalf("read-only store persisted a save")
	}
	if e, ok, _ := ro.Lookup(context.Background(), "u1"); !ok || e.MASID != "m1" {
		t.Fatalf("read-only store must still read, got %+v ok=%v", e, ok)
	}
}

// The masProfile-label-change path parks the user without dropping its MAS
// ID or snapshot, logs once, and resumes when the label maps back.
func TestProfileMismatchKeepsSnapshotAndResumes(t *testing.T) {
	store, _ := state.NewStore("memory", state.Options{DefaultProfileID: "p-old"})
	snap := state.Snapshot{UserName: "alice", HasActive: true, Active: true, ProfileID: "p-old"}
	_ = store.Save(context.Background(), "kc1", state.Entry{MASID: "mas1", ProfileID: "p-old", Status: state.StatusOK, Username: "alice", LastApplied: snap, CreatedByBridge: true})

	logger := &countingLogger{}
	resolver := NewProfileResolver("p-old", map[string]string{"users": "p-new"}, false)
	planner := NewPlanner(store, resolver, logger)
	user := keycloak.User{ID: "kc1", Username: "alice", MasProfile: "users", Enabled: true}

	for i := 0; i < 3; i++ {
		if actions := planner.Plan(context.Background(), []keycloak.User{user}); len(actions) != 0 {
			t.Fatalf("expected mismatch to skip, got %d actions", len(actions))
		}
	}
	if logger.errors != 1 {
		t.Fatalf("expected mismatch logged once, got %d", logger.errors)
	}
	e, _, _ := store.Lookup(context.Background(), "kc1")
	if e.MASID != "mas1" || e.LastApplied != snap || !e.CreatedByBridge || !e.Parked || e.ErrorClass != state.ErrorClassProfileMismatch {
		t.Fatalf("mismatch must park and keep MAS ID/snapshot/ownership, got %+v", e)
	}

	// Label reverts: the user is planned and executes normally again.
	user.MasProfile = ""
	actions := planner.Plan(context.Background(), []keycloak.User{user})
	if len(actions) != 1 || actions[0].Type != ActionUpdate {
		t.Fatalf("expected update after label revert, got %+v", actions)
	}
	fake := &scriptedMAS{}
	exec := NewExecutor(fake, store, false, true, stubLogger{})
	if err := exec.Execute(context.Background(), actions[0]); err != nil {
		t.Fatal(err)
	}
	e, _, _ = store.Lookup(context.Background(), "kc1")
	if e.Status != state.StatusOK || e.Parked {
		t.Fatalf("expected entry healthy after revert, got %+v", e)
	}
}

type countingLogger struct{ errors int }

func (c *countingLogger) Info(string, ...any)  {}
func (c *countingLogger) Warn(string, ...any)  {}
func (c *countingLogger) Error(string, ...any) { c.errors++ }
