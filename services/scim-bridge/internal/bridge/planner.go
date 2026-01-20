package bridge

import (
	"context"
	"fmt"

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
	ActionCreate ActionType = "create"
	ActionUpdate ActionType = "update"
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
