package bridge

import (
	"context"
	"fmt"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/keycloak"
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
	Warn(msg string, args ...any)
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
	// `users` is the resolved scoped set; removal detection below diffs it
	// against the state store.
	users := p.scope.filter(candidates)
	if len(users) == 0 {
		if len(candidates) == 0 {
			p.logger.Info("no users returned from Keycloak", "realm", p.kc.Realm())
		} else {
			p.logger.Info("no users matched filters", "realm", p.kc.Realm())
		}
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
	// Removal detection runs in group mode only: group membership is paged
	// fully, so the scoped set is complete and safe to diff against state.
	// The legacy ListUsers path fetches a single 50-user page — diffing state
	// against that page would mass-deactivate every tracked user beyond page
	// one, so it must never feed removal detection.
	if len(p.scope.includeGroups) == 0 {
		return nil
	}
	return p.reconcileRemovals(ctx, users)
}

// reconcileRemovals deactivates tracked users that fell out of the scoped
// set. It only runs after the scoped set resolved without error: any Keycloak
// failure has already aborted the cycle, so an empty set here means the
// groups are genuinely empty — valid, but worth a loud line before acting.
func (p *Poller) reconcileRemovals(ctx context.Context, scoped []keycloak.User) error {
	actions, err := p.planner.PlanRemovals(ctx, scoped)
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return nil
	}
	if len(scoped) == 0 {
		p.logger.Warn("scoped set is empty; deactivating every tracked user",
			"realm", p.kc.Realm(),
			"deactivations", len(actions),
		)
	}
	for _, action := range actions {
		p.logger.Info("plan action",
			"action", action.Type,
			"username", action.User.Username,
			"id", action.User.ID,
			"mas_id", action.ExistingMAS,
			"mas_profile_id", action.ProfileID,
		)
		if err := p.executor.Execute(ctx, action); err != nil {
			return fmt.Errorf("execute action: %w", err)
		}
	}
	return nil
}
