package bridge

import (
	"context"
	"strings"
	"testing"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

func TestPlannerResolvesProfileFromLabel(t *testing.T) {
	store, err := state.NewStore("memory", state.Options{DefaultProfileID: "default"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	resolver := NewProfileResolver("default", map[string]string{"users": "test1"}, false)
	planner := NewPlanner(store, resolver, stubLogger{})

	users := []keycloak.User{{
		ID:         "kc1",
		Username:   "alice",
		MasProfile: "users",
	}}
	actions := planner.Plan(context.Background(), users)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	action := actions[0]
	if action.ProfileID != "test1" || action.ProfileLabel != "users" {
		t.Fatalf("unexpected profile resolution: %+v", action)
	}
	if action.Type != ActionCreate {
		t.Fatalf("expected create action, got %s", action.Type)
	}
}

func TestPlannerStrictModeSkipsMissingLabel(t *testing.T) {
	store, err := state.NewStore("memory", state.Options{DefaultProfileID: "default"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	resolver := NewProfileResolver("default", map[string]string{"users": "test1"}, true)
	planner := NewPlanner(store, resolver, stubLogger{})

	users := []keycloak.User{{ID: "kc1", Username: "alice"}}
	actions := planner.Plan(context.Background(), users)
	if len(actions) != 0 {
		t.Fatalf("expected no actions when label is missing in strict mode, got %d", len(actions))
	}
}

func TestPlannerMarksProfileMismatch(t *testing.T) {
	store, err := state.NewStore("memory", state.Options{DefaultProfileID: "default"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	_ = store.Save(context.Background(), "kc1", state.Entry{
		MASID:     "mas1",
		ProfileID: "p-old",
		Status:    state.StatusOK,
		Username:  "alice",
	})

	resolver := NewProfileResolver("default", map[string]string{"users": "p-new"}, false)
	planner := NewPlanner(store, resolver, stubLogger{})
	actions := planner.Plan(context.Background(), []keycloak.User{{
		ID:         "kc1",
		Username:   "alice",
		MasProfile: "users",
	}})

	if len(actions) != 0 {
		t.Fatalf("expected mismatch to skip actions, got %d", len(actions))
	}

	entry, _, _ := store.Lookup(context.Background(), "kc1")
	if entry.Status != state.StatusError {
		t.Fatalf("expected status error after mismatch, got %s", entry.Status)
	}
	if !strings.Contains(entry.LastError, "mismatch") {
		t.Fatalf("expected mismatch message, got %q", entry.LastError)
	}
}
