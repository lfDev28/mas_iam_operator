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
}

// Plan currently emits create for unseen IDs and update for known IDs.
func (p *Planner) Plan(ctx context.Context, users []keycloak.User) []Action {
	actions := make([]Action, 0, len(users))
	for _, u := range users {
		profileResolution, ok := p.resolver.Resolve(u.MasProfile)
		if !ok {
			p.logger.Info("skip user without mapped masProfile", "username", u.Username, "id", u.ID, "mas_profile", u.MasProfile)
			continue
		}

		entry, hasEntry, _ := p.store.Lookup(ctx, u.ID)
		if hasEntry && entry.ProfileID != "" && entry.ProfileID != profileResolution.ProfileID {
			msg := fmt.Sprintf("mas profile mismatch: stored=%s derived=%s", entry.ProfileID, profileResolution.ProfileID)
			p.logger.Error("profile mismatch; skipping user", "username", u.Username, "id", u.ID, "stored_profile", entry.ProfileID, "derived_profile", profileResolution.ProfileID, "mas_profile_label", profileResolution.Label)
			_ = p.store.Save(ctx, u.ID, state.Entry{
				MASID:     entry.MASID,
				ProfileID: entry.ProfileID,
				Status:    state.StatusError,
				LastError: msg,
				Username:  u.Username,
			})
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
		}

		if hasEntry {
			action.Type = ActionUpdate
			action.ExistingMAS = entry.MASID
			action.StateStatus = entry.Status
			action.StateLastError = entry.LastError
			action.StateLastApplied = entry.LastApplied
		} else {
			action.Type = ActionCreate
		}
		actions = append(actions, action)
	}
	return actions
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
		// Never materialized in MAS (e.g. failed create); nothing to deactivate.
		if entry.MASID == "" {
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
		})
	}
	return actions, nil
}
