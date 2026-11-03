package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// state.json written by pre-v0.1.16 bridges has none of the retry fields;
// it must load, and its error entries must read as "retry now".
func TestLoadFromFileLegacyErrorEntriesAreRetryable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	legacy := `{
  "kc1": {"masID": "", "profileID": "p", "status": "error", "lastError": "500 Internal Server Error: ", "username": "alice", "lastApplied": {"active": false}},
  "kc2": {"masID": "mas2", "profileID": "p", "status": "ok", "username": "bob", "lastApplied": {"userName": "bob", "active": true, "hasActive": true, "profileID": "p"}}
}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadFromFile(path, "p")
	if err != nil {
		t.Fatalf("load legacy state: %v", err)
	}
	e := entries["kc1"]
	if e.Status != StatusError || e.Attempts != 0 || !e.NextRetryAt.IsZero() || e.Parked || e.DesiredHash != "" || e.CreatedByBridge {
		t.Fatalf("legacy error entry should load with zero retry fields, got %+v", e)
	}
	if entries["kc2"].MASID != "mas2" || entries["kc2"].CreatedByBridge {
		t.Fatalf("legacy ok entry mis-loaded: %+v", entries["kc2"])
	}
}

func TestRetryFieldsRoundTripAndOkEntriesStayCompact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	retryAt := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	in := map[string]Entry{
		"kc1": {Status: StatusError, ProfileID: "p", Attempts: 3, NextRetryAt: retryAt, ErrorClass: ErrorClassPermanent, Parked: true, DesiredHash: "abc", LastError: "400"},
		// An ok entry with leftover retry fields is normalized clean.
		"kc2": {MASID: "m2", Status: StatusOK, ProfileID: "p", Attempts: 2, NextRetryAt: retryAt, LastError: "x", CreatedByBridge: true},
	}
	if err := WriteToFile(path, in, "p"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "nextRetryAt") != 1 {
		t.Fatalf("expected nextRetryAt only on the failed entry, got:\n%s", raw)
	}
	out, err := LoadFromFile(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	e := out["kc1"]
	if e.Attempts != 3 || !e.NextRetryAt.Equal(retryAt) || e.ErrorClass != ErrorClassPermanent || !e.Parked || e.DesiredHash != "abc" {
		t.Fatalf("retry fields lost in round trip: %+v", e)
	}
	ok := out["kc2"]
	if ok.Attempts != 0 || !ok.NextRetryAt.IsZero() || ok.LastError != "" || !ok.CreatedByBridge {
		t.Fatalf("ok entry should be normalized clean and keep ownership, got %+v", ok)
	}
}

func TestHasMASUserIgnoresEmptyAndDryRunIDs(t *testing.T) {
	for id, want := range map[string]bool{"": false, "dryrun-kc1": false, "abc123": true} {
		if got := (Entry{MASID: id}).HasMASUser(); got != want {
			t.Fatalf("HasMASUser(%q) = %v, want %v", id, got, want)
		}
	}
}
