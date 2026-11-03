package bridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
)

// keycloakSource is the slice of the Keycloak client the scoping logic needs.
type keycloakSource interface {
	ListUsersPage(ctx context.Context, first, max int) ([]keycloak.User, int, error)
	ListGroups(ctx context.Context, search string) ([]keycloak.Group, error)
	ListGroupMembers(ctx context.Context, groupID string, first, max int) ([]keycloak.User, int, error)
	Realm() string
}

// groupMembersPageSize and usersPageSize bound each request; the page loops
// keep going until Keycloak returns a short page, so neither scoped set is
// capped at a single page.
const (
	groupMembersPageSize = 50
	usersPageSize        = 100
	// maxPages stops a runaway loop (e.g. a proxy that ignores `first`
	// and returns the same full page forever) instead of spinning.
	maxPages = 10000
)

// userScope resolves which Keycloak users the bridge is allowed to touch.
// Groups (when configured) select the candidate set; the username filters
// then narrow it (AND). Removal detection diffs the resolved set against
// the state store, so candidates/filter keep it an explicit value rather
// than a side effect.
type userScope struct {
	kc               keycloakSource
	includeUsernames map[string]struct{}
	includePrefix    string
	includeGroups    []string
}

// Keycloak stores usernames lowercased, so the username allowlist and
// prefix are matched case-insensitively: "Scim.User1" in config must still
// match Keycloak's "scim.user1".
func newUserScope(kc keycloakSource, includeUsernames []string, includePrefix string, includeGroups []string) userScope {
	includeSet := make(map[string]struct{}, len(includeUsernames))
	for _, u := range includeUsernames {
		includeSet[strings.ToLower(u)] = struct{}{}
	}
	return userScope{
		kc:               kc,
		includeUsernames: includeSet,
		includePrefix:    strings.ToLower(includePrefix),
		includeGroups:    includeGroups,
	}
}

// candidates sources the pre-filter user set: group membership when groups
// are configured, otherwise every realm user (paged).
func (s *userScope) candidates(ctx context.Context) ([]keycloak.User, error) {
	if len(s.includeGroups) == 0 {
		return s.realmUsers(ctx)
	}
	return s.groupMembers(ctx)
}

// realmUsers pages through the realm user list until a short page.
func (s *userScope) realmUsers(ctx context.Context) ([]keycloak.User, error) {
	seen := make(map[string]struct{})
	var users []keycloak.User
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("list users: gave up after %d pages; is Keycloak honouring the `first` parameter?", maxPages)
		}
		batch, rawCount, err := s.kc.ListUsersPage(ctx, page*usersPageSize, usersPageSize)
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		for _, u := range batch {
			if _, dup := seen[u.ID]; dup {
				continue
			}
			seen[u.ID] = struct{}{}
			users = append(users, u)
		}
		if rawCount < usersPageSize {
			return users, nil
		}
	}
}

// groupMembers unions the direct members of every configured group,
// deduplicated by Keycloak user ID.
func (s *userScope) groupMembers(ctx context.Context) ([]keycloak.User, error) {
	seen := make(map[string]struct{})
	var members []keycloak.User
	for _, name := range s.includeGroups {
		group, err := s.resolveGroup(ctx, name)
		if err != nil {
			return nil, err
		}
		for first := 0; ; first += groupMembersPageSize {
			if first/groupMembersPageSize >= maxPages {
				return nil, fmt.Errorf("list members of group %q: gave up after %d pages", name, maxPages)
			}
			page, rawCount, err := s.kc.ListGroupMembers(ctx, group.ID, first, groupMembersPageSize)
			if err != nil {
				return nil, fmt.Errorf("list members of group %q: %w", name, err)
			}
			for _, u := range page {
				if _, dup := seen[u.ID]; dup {
					continue
				}
				seen[u.ID] = struct{}{}
				members = append(members, u)
			}
			if rawCount < groupMembersPageSize {
				break
			}
		}
	}
	return members, nil
}

// resolveGroup maps a configured group name or path to a Keycloak group.
// An unresolvable name fails the cycle: a typo must not silently sync
// nobody.
func (s *userScope) resolveGroup(ctx context.Context, name string) (keycloak.Group, error) {
	// Keycloak's search matches on group name, so search by the last path
	// segment to let full paths (e.g. /parent/child) resolve too.
	search := name
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		search = name[idx+1:]
	}
	groups, err := s.kc.ListGroups(ctx, search)
	if err != nil {
		return keycloak.Group{}, fmt.Errorf("resolve group %q: %w", name, err)
	}
	for _, g := range groups {
		if g.Name == name || g.Path == name {
			return g, nil
		}
	}
	return keycloak.Group{}, fmt.Errorf("group %q not found in realm %s", name, s.kc.Realm())
}

// filter applies the username allowlist and prefix filters (AND).
func (s *userScope) filter(users []keycloak.User) []keycloak.User {
	if len(s.includeUsernames) == 0 && s.includePrefix == "" {
		return users
	}
	filtered := make([]keycloak.User, 0, len(users))
	for _, user := range users {
		name := strings.ToLower(user.Username)
		if len(s.includeUsernames) > 0 {
			if _, ok := s.includeUsernames[name]; !ok {
				continue
			}
		}
		if s.includePrefix != "" && !strings.HasPrefix(name, s.includePrefix) {
			continue
		}
		filtered = append(filtered, user)
	}
	return filtered
}
