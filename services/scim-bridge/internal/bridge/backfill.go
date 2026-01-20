package bridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

type keycloakLister interface {
	ListUsers(ctx context.Context, params keycloak.ListUsersParams) ([]keycloak.User, error)
	Realm() string
}

// Backfill seeds the state store with existing MAS IDs discovered via SCIM search.
type Backfill struct {
	kc       keycloakLister
	resolver ProfileResolver
	store    state.Store
	logger   Logger

	includeUsernames      map[string]struct{}
	includeUsernamePrefix string
	masClient             MASClient
}

// NewBackfill constructs a state backfill worker.
func NewBackfill(kc keycloakLister, resolver ProfileResolver, store state.Store, includeUsernames []string, includePrefix string, logger Logger) *Backfill {
	includeSet := make(map[string]struct{}, len(includeUsernames))
	for _, u := range includeUsernames {
		includeSet[u] = struct{}{}
	}
	return &Backfill{
		kc:                    kc,
		resolver:              resolver,
		store:                 store,
		logger:                logger,
		includeUsernames:      includeSet,
		includeUsernamePrefix: includePrefix,
	}
}

// Attach MAS client after creation (needed to avoid cycles).
func (b *Backfill) WithMASClient(m MASClient) *Backfill {
	b.masClient = m
	return b
}

// Run performs a single backfill pass.
func (b *Backfill) Run(ctx context.Context) error {
	if b.masClient == nil {
		return fmt.Errorf("mas client not configured for backfill")
	}
	users, err := b.kc.ListUsers(ctx, keycloak.ListUsersParams{Max: 50})
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	users = b.filterUsers(users)
	for _, u := range users {
		profileRes, ok := b.resolver.Resolve(u.MasProfile)
		if !ok {
			b.logger.Info("skip user without mapped masProfile during backfill", "username", u.Username, "id", u.ID, "mas_profile", u.MasProfile)
			continue
		}

		masID, searchFilter, err := b.searchMASUser(ctx, profileRes.ProfileID, u.ID, u.Username)
		if err != nil {
			return fmt.Errorf("search MAS for user %s: %w", u.Username, err)
		}

		switch masID {
		case "":
			msg := fmt.Sprintf("no unique MAS user found (filter=%s)", searchFilter)
			b.logger.Info("backfill could not map user", "username", u.Username, "id", u.ID, "mas_profile_id", profileRes.ProfileID, "filter", searchFilter)
			_ = b.store.Save(ctx, u.ID, state.Entry{
				MASID:     "",
				ProfileID: profileRes.ProfileID,
				Status:    state.StatusError,
				LastError: msg,
				Username:  u.Username,
			})
		default:
			b.logger.Info("backfill mapped user", "username", u.Username, "id", u.ID, "mas_id", masID, "mas_profile_id", profileRes.ProfileID, "filter", searchFilter)
			_ = b.store.Save(ctx, u.ID, state.Entry{
				MASID:     masID,
				ProfileID: profileRes.ProfileID,
				Status:    state.StatusOK,
				Username:  u.Username,
			})
		}
	}
	return nil
}

func (b *Backfill) filterUsers(users []keycloak.User) []keycloak.User {
	if len(b.includeUsernames) == 0 && b.includeUsernamePrefix == "" {
		return users
	}
	filtered := make([]keycloak.User, 0, len(users))
	for _, user := range users {
		if len(b.includeUsernames) > 0 {
			if _, ok := b.includeUsernames[user.Username]; !ok {
				continue
			}
		}
		if b.includeUsernamePrefix != "" && !strings.HasPrefix(user.Username, b.includeUsernamePrefix) {
			continue
		}
		filtered = append(filtered, user)
	}
	return filtered
}

func (b *Backfill) searchMASUser(ctx context.Context, profileID, keycloakID, username string) (masID string, filterUsed string, err error) {
	filter := fmt.Sprintf(`externalId eq "%s"`, keycloakID)
	users, err := b.masClient.SearchUsers(ctx, profileID, filter)
	if err != nil {
		// fallback via username
		filter = fmt.Sprintf(`userName eq "%s"`, username)
		users, err = b.masClient.SearchUsers(ctx, profileID, filter)
		if err != nil {
			return "", filter, err
		}
	}
	if len(users) == 0 {
		filter = fmt.Sprintf(`userName eq "%s"`, username)
		users, err = b.masClient.SearchUsers(ctx, profileID, filter)
		if err != nil {
			return "", filter, err
		}
	}
	if len(users) == 1 {
		return users[0].ID, filter, nil
	}
	return "", filter, nil
}
