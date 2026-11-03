package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

func fixtureEntries() map[string]state.Entry {
	return map[string]state.Entry{
		// healthy
		"ok1": {MASID: "m-ok", ProfileID: "p", Status: state.StatusOK, CreatedByBridge: true},
		// failed create, nothing in MAS
		"nocreate": {MASID: "", ProfileID: "p", Status: state.StatusError, LastError: "500", ErrorClass: state.ErrorClassTransient, Attempts: 4, NextRetryAt: time.Now().Add(time.Hour)},
		// healthy bridge user with one transient PATCH failure: retrying
		"retrying": {MASID: "m-retry", ProfileID: "p", Status: state.StatusError, ErrorClass: state.ErrorClassTransient, Attempts: 1, CreatedByBridge: true},
		// parked, bridge-created: the only deletable kind
		"parked-own": {MASID: "m-own", ProfileID: "p", Status: state.StatusError, ErrorClass: state.ErrorClassPermanent, Parked: true, Attempts: 3, DesiredHash: "h", CreatedByBridge: true},
		// parked, but MAS user was not created by us (legacy entry / adopted)
		"parked-foreign": {MASID: "m-foreign", ProfileID: "p", Status: state.StatusError, ErrorClass: state.ErrorClassPermanent, Parked: true, DesiredHash: "h"},
		// legacy dry-run correlation
		"dry": {MASID: "dryrun-dry", ProfileID: "p", Status: state.StatusOK},
	}
}

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := state.WriteToFile(path, fixtureEntries(), "p"); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResetStateClearsErrorsWithoutTouchingMAS(t *testing.T) {
	path := writeFixture(t)
	if err := run([]string{"--state-path", path, "--reset-state"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := state.LoadFromFile(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["nocreate"]; ok {
		t.Fatalf("entry without MAS user should be dropped so the bridge creates it")
	}
	if _, ok := got["dry"]; ok {
		t.Fatalf("dry-run correlation should be dropped")
	}
	for _, id := range []string{"retrying", "parked-own", "parked-foreign"} {
		e, ok := got[id]
		if !ok || e.MASID == "" {
			t.Fatalf("%s: correlation must be kept, got %+v ok=%v", id, e, ok)
		}
		if e.Parked || e.Attempts != 0 || !e.NextRetryAt.IsZero() || e.DesiredHash != "" {
			t.Fatalf("%s: retry bookkeeping not cleared: %+v", id, e)
		}
	}
	if got["ok1"].Status != state.StatusOK {
		t.Fatalf("healthy entry changed: %+v", got["ok1"])
	}
}

func TestDeleteOnlyRemovesParkedBridgeCreatedUsers(t *testing.T) {
	var mu sync.Mutex
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		deleted = append(deleted, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	path := writeFixture(t)
	if err := run([]string{"--state-path", path, "--delete", "--mas-base-url", srv.URL + "/scim/v2", "--mas-profile-id", "p", "--mas-token", "t"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	sort.Strings(deleted)
	if len(deleted) != 1 || deleted[0] != "/scim/v2/p/Users/m-own" {
		t.Fatalf("expected only the parked bridge-created user deleted, got %v", deleted)
	}
	got, _ := state.LoadFromFile(path, "p")
	if _, ok := got["parked-own"]; ok {
		t.Fatalf("deleted entry should be removed from state")
	}
	for _, id := range []string{"retrying", "parked-foreign", "nocreate"} {
		if _, ok := got[id]; !ok {
			t.Fatalf("%s must be left alone by --delete", id)
		}
	}
}

func TestDeletionDecision(t *testing.T) {
	for id, e := range fixtureEntries() {
		ok, reason := deletionDecision(e)
		if want := id == "parked-own"; ok != want {
			t.Fatalf("%s: deletable=%v (%s), want %v", id, ok, reason, want)
		}
	}
}

func TestResetAndDeleteAreMutuallyExclusive(t *testing.T) {
	if err := run([]string{"--state-path", writeFixture(t), "--reset-state", "--delete"}); err == nil {
		t.Fatalf("expected error when both modes are set")
	}
}
