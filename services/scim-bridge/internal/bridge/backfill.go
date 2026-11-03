package bridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// Backfill seeds the state store with existing MAS IDs discovered via SCIM search.
type Backfill struct {
	resolver ProfileResolver
	store    state.Store
	logger   Logger

	scope     userScope
	masClient MASClient
}

// NewBackfill constructs a state backfill worker.
func NewBackfill(kc keycloakSource, resolver ProfileResolver, store state.Store, includeUsernames []string, includePrefix string, includeGroups []string, logger Logger) *Backfill {
	return &Backfill{
		resolver: resolver,
		store:    store,
		logger:   logger,
		scope:    newUserScope(kc, includeUsernames, includePrefix, includeGroups),
	}
}

// Attach MAS client after creation (needed to avoid cycles).
func (b *Backfill) WithMASClient(m MASClient) *Backfill {
	b.masClient = m
	return b
}

// Run performs a single backfill pass.
//
// Backfill deliberately performs no removal detection: it is a bootstrap
// sweep that seeds correlations for the current scoped set. Deactivating
// users absent from the set is the steady-state poller's job (group mode
// only) — a one-shot backfill run against a partially-populated group must
// not deactivate anyone.
func (b *Backfill) Run(ctx context.Context) error {
	if b.masClient == nil {
		return fmt.Errorf("mas client not configured for backfill")
	}
	candidates, err := b.scope.candidates(ctx)
	if err != nil {
		return err
	}
	users := b.scope.filter(candidates)
	for _, u := range users {
		profileRes, ok := b.resolver.Resolve(u.MasProfile)
		if !ok {
			b.logger.Info("skip user without mapped masProfile during backfill", "username", u.Username, "id", u.ID, "mas_profile", u.MasProfile)
			continue
		}

		masID, searchFilter, byExternalID, err := b.searchMASUser(ctx, profileRes.ProfileID, u.ID, u.Username)
		if err != nil {
			return fmt.Errorf("search MAS for user %s: %w", u.Username, err)
		}

		if masID == "" {
			// No unique MAS user: leave state alone. Writing a status=error
			// entry here used to strand the user; with no entry the poller
			// simply creates it (and resolves any 409 itself).
			b.logger.Info("backfill could not map user; the poller will create it", "username", u.Username, "id", u.ID, "mas_profile_id", profileRes.ProfileID, "filter", searchFilter)
			continue
		}
		b.logger.Info("backfill mapped user", "username", u.Username, "id", u.ID, "mas_id", masID, "mas_profile_id", profileRes.ProfileID, "filter", searchFilter)
		// Backfill is an explicit operator action, so a userName match is
		// accepted here, but only an externalId match marks the MAS user as
		// bridge-owned (the only kind cleanup will ever delete).
		_ = b.store.Save(ctx, u.ID, state.Entry{
			MASID:           masID,
			ProfileID:       profileRes.ProfileID,
			Status:          state.StatusOK,
			Username:        u.Username,
			CreatedByBridge: byExternalID,
		})
	}
	return nil
}

func (b *Backfill) searchMASUser(ctx context.Context, profileID, keycloakID, username string) (masID string, filterUsed string, byExternalID bool, err error) {
	filter := fmt.Sprintf(`externalId eq "%s"`, keycloakID)
	users, err := b.masClient.SearchUsers(ctx, profileID, filter)
	if err == nil && len(users) == 1 && (users[0].ExternalID == "" || strings.EqualFold(users[0].ExternalID, keycloakID)) {
		return users[0].ID, filter, true, nil
	}
	filter = fmt.Sprintf(`userName eq "%s"`, username)
	users, err = b.masClient.SearchUsers(ctx, profileID, filter)
	if err != nil {
		return "", filter, false, err
	}
	if len(users) == 1 {
		return users[0].ID, filter, strings.EqualFold(users[0].ExternalID, keycloakID), nil
	}
	return "", filter, false, nil
}
