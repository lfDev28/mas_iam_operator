package bridge

import (
	"context"
	"fmt"
	"sort"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// Planner determines which actions to take based on Keycloak + stored MAS state.
type Planner struct {
	store    state.Store
	resolver ProfileResolver
	logger   Logger
}

// NewPlanner creates a planner instance with the given state backend.
func NewPlanner(store state.Store, resolver ProfileResolver, logger Logger) *Planner {
	return &Planner{store: store, resolver: resolver, logger: logger}
}

// ActionType enumerates the types of reconciliation steps we may take.
type ActionType string

const (
	ActionCreate     ActionType = "create"
	ActionUpdate     ActionType = "update"
	ActionDeactivate ActionType = "deactivate"
)

// Action pairs a Keycloak user with an intended MAS operation.
type Action struct {
	Type             ActionType
	User             keycloak.User
	ExistingMAS      string
	StateStatus      string
	StateLastError   string
	StateLastApplied state.Snapshot
	ProfileLabel     string
	ProfileID        string
	// Prior is the full state entry when HasPrior; the executor uses it for
	// retry gating and to carry ownership/snapshot through outcomes.
	Prior    state.Entry
	HasPrior bool
}

// Plan emits create for users with no MAS user yet (unseen, failed create,
// or a pre-v0.1.16 dry-run ID) and update for users correlated to a MAS ID.
// Retry gating for failed entries happens in the executor.
func (p *Planner) Plan(ctx context.Context, users []keycloak.User) []Action {
	actions := make([]Action, 0, len(users))
	for _, u := range users {
		profileResolution, ok := p.resolver.Resolve(u.MasProfile)
		if !ok {
			p.logger.Info("skip user without mapped masProfile", "username", u.Username, "id", u.ID, "mas_profile", u.MasProfile)
			continue
		}

		entry, hasEntry, _ := p.store.Lookup(ctx, u.ID)
		if hasEntry && entry.HasMASUser() && entry.ProfileID != "" && entry.ProfileID != profileResolution.ProfileID {
			p.recordProfileMismatch(ctx, u, entry, profileResolution)
			continue
		}
		if hasEntry && entry.ProfileID == "" {
			entry.ProfileID = profileResolution.ProfileID
			_ = p.store.Save(ctx, u.ID, entry)
		}

		action := Action{
			User:         u,
			ProfileLabel: profileResolution.Label,
			ProfileID:    profileResolution.ProfileID,
			Prior:        entry,
			HasPrior:     hasEntry,
		}
		if hasEntry {
			action.StateStatus = entry.Status
			action.StateLastError = entry.LastError
			action.StateLastApplied = entry.LastApplied
		}
		if hasEntry && entry.HasMASUser() {
			action.Type = ActionUpdate
			action.ExistingMAS = entry.MASID
		} else {
			action.Type = ActionCreate
		}
		actions = append(actions, action)
	}
	return actions
}

// recordProfileMismatch parks a user whose masProfile label now maps to a
// different MAS profile than the one its MAS user lives in. Moving users
// between profiles is not supported, so retrying cannot help; the entry
// keeps its MAS ID, snapshot and ownership, and syncs again as soon as the
// label (or the profile map) resolves back to the stored profile. Logged
// once per distinct mismatch rather than every cycle.
func (p *Planner) recordProfileMismatch(ctx context.Context, u keycloak.User, entry state.Entry, res ProfileResolution) {
	marker := "profile-mismatch:" + res.ProfileID
	if entry.Status == state.StatusError && entry.Parked && entry.DesiredHash == marker {
		return
	}
	msg := fmt.Sprintf("mas profile mismatch: stored=%s derived=%s", entry.ProfileID, res.ProfileID)
	p.logger.Error("profile mismatch; parking user until its masProfile label maps back to the stored profile",
		"username", u.Username, "id", u.ID, "stored_profile", entry.ProfileID, "derived_profile", res.ProfileID, "mas_profile_label", res.Label)
	entry.Status = state.StatusError
	entry.LastError = msg
	entry.ErrorClass = state.ErrorClassProfileMismatch
	entry.Parked = true
	entry.DesiredHash = marker
	entry.Attempts = 1
	entry.Username = u.Username
	_ = p.store.Save(ctx, u.ID, entry)
}

// PlanRemovals emits deactivate actions for users tracked in state but absent
// from the resolved scoped set. Callers must only feed it a scoped set sourced
// from group membership (the poller gates this); the legacy ListUsers path is
// a single page and diffing against it would deactivate off-page users.
func (p *Planner) PlanRemovals(ctx context.Context, scoped []keycloak.User) ([]Action, error) {
	entries, err := p.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list state entries: %w", err)
	}
	present := make(map[string]struct{}, len(scoped))
	for _, u := range scoped {
		present[u.ID] = struct{}{}
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var actions []Action
	for _, kcID := range ids {
		if _, ok := present[kcID]; ok {
			continue
		}
		entry := entries[kcID]
		// Tombstoned: deactivation already applied, fire once only.
		if entry.Status == state.StatusDeactivated {
			continue
		}
		// Never materialized in MAS (failed or dry-run create); nothing to
		// deactivate.
		if !entry.HasMASUser() {
			continue
		}
		actions = append(actions, Action{
			Type:             ActionDeactivate,
			User:             keycloak.User{ID: kcID, Username: entry.Username},
			ExistingMAS:      entry.MASID,
			StateStatus:      entry.Status,
			StateLastError:   entry.LastError,
			StateLastApplied: entry.LastApplied,
			ProfileID:        entry.ProfileID,
			Prior:            entry,
			HasPrior:         true,
		})
	}
	return actions, nil
}
