package bridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// Poller performs a single pass reconciliation between Keycloak and MAS.
type Poller struct {
	kc                    *keycloak.Client
	planner               *Planner
	executor              *Executor
	logger                Logger
	includeUsernames      map[string]struct{}
	includeUsernamePrefix string
}

// Logger captures the logging calls we rely on.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewPoller wires dependencies for the polling reconciliation loop.
func NewPoller(kc *keycloak.Client, planner *Planner, executor *Executor, includeUsernames []string, includePrefix string, logger Logger) *Poller {
	includeSet := make(map[string]struct{}, len(includeUsernames))
	for _, u := range includeUsernames {
		includeSet[u] = struct{}{}
	}
	return &Poller{
		kc:                    kc,
		planner:               planner,
		executor:              executor,
		logger:                logger,
		includeUsernames:      includeSet,
		includeUsernamePrefix: includePrefix,
	}
}

// RunOnce fetches a single page of Keycloak users and logs the intended action.
func (p *Poller) RunOnce(ctx context.Context) error {
	users, err := p.kc.ListUsers(ctx, keycloak.ListUsersParams{Max: 50})
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	originalCount := len(users)
	users = p.filterUsers(users)
	if len(users) == 0 {
		if originalCount == 0 {
			p.logger.Info("no users returned from Keycloak", "realm", p.kc.Realm())
		} else {
			p.logger.Info("no users matched filters", "realm", p.kc.Realm())
		}
		return nil
	}
	actions := p.planner.Plan(ctx, users)
	for _, action := range actions {
		if action.Type == ActionUpdate && action.StateStatus == state.StatusError {
			p.logger.Info("skip update due to prior MAS error",
				"username", action.User.Username,
				"id", action.User.ID,
				"mas_profile_label", action.ProfileLabel,
				"mas_profile_id", action.ProfileID,
				"existing_mas_id", action.ExistingMAS,
				"last_error", action.StateLastError,
			)
			continue
		}
		p.logger.Info("plan action",
			"action", action.Type,
			"username", action.User.Username,
			"id", action.User.ID,
			"mas_profile_label", action.ProfileLabel,
			"mas_profile_id", action.ProfileID,
			"existing_mas_id", action.ExistingMAS,
		)
		if err := p.executor.Execute(ctx, action); err != nil {
			return fmt.Errorf("execute action: %w", err)
		}
	}
	return nil
}

func (p *Poller) filterUsers(users []keycloak.User) []keycloak.User {
	if len(p.includeUsernames) == 0 && p.includeUsernamePrefix == "" {
		return users
	}
	filtered := make([]keycloak.User, 0, len(users))
	for _, user := range users {
		if len(p.includeUsernames) > 0 {
			if _, ok := p.includeUsernames[user.Username]; !ok {
				continue
			}
		}
		if p.includeUsernamePrefix != "" && !strings.HasPrefix(user.Username, p.includeUsernamePrefix) {
			continue
		}
		filtered = append(filtered, user)
	}
	return filtered
}
