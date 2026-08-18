package bridge

import (
	"context"
	"fmt"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// Poller performs a single pass reconciliation between Keycloak and MAS.
type Poller struct {
	kc       keycloakSource
	planner  *Planner
	executor *Executor
	logger   Logger
	scope    userScope
}

// Logger captures the logging calls we rely on.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewPoller wires dependencies for the polling reconciliation loop.
func NewPoller(kc keycloakSource, planner *Planner, executor *Executor, includeUsernames []string, includePrefix string, includeGroups []string, logger Logger) *Poller {
	return &Poller{
		kc:       kc,
		planner:  planner,
		executor: executor,
		logger:   logger,
		scope:    newUserScope(kc, includeUsernames, includePrefix, includeGroups),
	}
}

// RunOnce resolves the scoped Keycloak user set and executes the planned actions.
func (p *Poller) RunOnce(ctx context.Context) error {
	candidates, err := p.scope.candidates(ctx)
	if err != nil {
		return err
	}
	// `users` is the resolved scoped set; phase 2 (deactivate-on-removal)
	// will diff it against the state store.
	users := p.scope.filter(candidates)
	if len(users) == 0 {
		if len(candidates) == 0 {
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
