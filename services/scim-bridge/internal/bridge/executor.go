package bridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/state"
)

// MASClient captures the MAS operations needed by the executor.
type MASClient interface {
	CreateUser(ctx context.Context, profileID string, payload mas.UserResource) (string, error)
	UpdateUser(ctx context.Context, profileID, masID string, payload mas.UserResource) error
	PatchUser(ctx context.Context, profileID, masID string, ops []mas.PatchOperation) error
	SearchUsers(ctx context.Context, profileID, filter string) ([]mas.User, error)
}

// Executor will eventually perform MAS SCIM operations; for now it persists
// state transitions so we can track seen identities.
type Executor struct {
	mas          MASClient
	store        state.Store
	logger       Logger
	dryRun       bool
	allowUpdates bool
}

// ErrMASUnauthorized indicates MAS returned 401 and the caller may refresh credentials.
var ErrMASUnauthorized = errors.New("mas unauthorized")

// NewExecutor wires a new action executor.
func NewExecutor(masClient MASClient, store state.Store, dryRun bool, allowUpdates bool, logger Logger) *Executor {
	return &Executor{mas: masClient, store: store, dryRun: dryRun, allowUpdates: allowUpdates, logger: logger}
}

// SetMASClient swaps the runtime MAS client (used after token refresh).
func (e *Executor) SetMASClient(masClient MASClient) {
	e.mas = masClient
}

// Execute currently simulates the result of an action and updates correlation
// state. MAS calls are stubbed until the client gains real POST/PUT support.
func (e *Executor) Execute(ctx context.Context, action Action) error {
	payload := mas.BuildUserPayload(action.User)
	switch action.Type {
	case ActionCreate:
		if e.dryRun {
			masID := fmt.Sprintf("dryrun-%s", action.User.ID)
			e.logger.Info("dry-run create",
				"username", action.User.Username,
				"mas_id", masID,
				"mas_profile_label", action.ProfileLabel,
				"mas_profile_id", action.ProfileID,
			)
			return e.store.Save(ctx, action.User.ID, state.Entry{
				MASID:       masID,
				ProfileID:   action.ProfileID,
				Status:      state.StatusOK,
				Username:    action.User.Username,
				LastApplied: snapshotFromPayload(payload, action.ProfileID),
			})
		}
		masID, err := e.mas.CreateUser(ctx, action.ProfileID, payload)
		if err != nil {
			var respErr *mas.ResponseError
			if errors.As(err, &respErr) {
				if respErr.StatusCode == 401 {
					return wrapMASUnauthorized(err)
				}
				if respErr.StatusCode == 409 {
					return e.handleConflict(ctx, action, payload)
				}
				e.logger.Error("MAS create failed", "username", action.User.Username, "status", respErr.Status, "status_code", respErr.StatusCode, "response_body", respErr.Body, "mas_profile_id", action.ProfileID, "mas_profile_label", action.ProfileLabel)
				_ = e.store.Save(ctx, action.User.ID, state.Entry{
					MASID:     "",
					ProfileID: action.ProfileID,
					Status:    state.StatusError,
					LastError: fmt.Sprintf("%s: %s", respErr.Status, respErr.Body),
					Username:  action.User.Username,
				})
				return nil
			}
			return fmt.Errorf("create MAS user: %w", err)
		}
		if err := e.store.Save(ctx, action.User.ID, state.Entry{
			MASID:       masID,
			ProfileID:   action.ProfileID,
			Status:      state.StatusOK,
			Username:    action.User.Username,
			LastApplied: snapshotFromPayload(payload, action.ProfileID),
		}); err != nil {
			return fmt.Errorf("store correlation: %w", err)
		}
		return nil
	case ActionUpdate:
		if action.ExistingMAS == "" {
			return fmt.Errorf("missing MAS ID for update")
		}
		if !e.allowUpdates {
			e.logger.Info("updates disabled", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
			return nil
		}
		if e.dryRun {
			e.logger.Info("dry-run update", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
			return nil
		}
		return e.updateWithPatch(ctx, action, payload)
	case ActionDeactivate:
		return e.deactivate(ctx, action)
	default:
		e.logger.Info("unhandled action", "action", action.Type)
		return nil
	}
}

// deactivate sets SCIM active:false on the MAS user (never delete) and
// tombstones the state entry so the deactivation fires once. A failed MAS
// call must not tombstone: the entry keeps a non-deactivated status so the
// next cycle plans the deactivation again.
func (e *Executor) deactivate(ctx context.Context, action Action) error {
	if action.ExistingMAS == "" {
		return fmt.Errorf("missing MAS ID for deactivate")
	}
	if !e.allowUpdates {
		e.logger.Info("updates disabled; skipping deactivate", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
		return nil
	}
	tombstone := state.Entry{
		MASID:       action.ExistingMAS,
		ProfileID:   action.ProfileID,
		Status:      state.StatusDeactivated,
		Username:    action.User.Username,
		LastApplied: deactivatedSnapshot(action.StateLastApplied),
	}
	if e.dryRun {
		e.logger.Info("dry-run deactivate", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
		return e.store.Save(ctx, action.User.ID, tombstone)
	}
	ops := []mas.PatchOperation{{Op: "replace", Path: "active", Value: false}}
	if err := e.mas.PatchUser(ctx, action.ProfileID, action.ExistingMAS, ops); err != nil {
		var respErr *mas.ResponseError
		if errors.As(err, &respErr) {
			switch respErr.StatusCode {
			case 401:
				return wrapMASUnauthorized(err)
			case 404:
				// Already gone from MAS; tombstone so we stop retrying.
				e.logger.Info("MAS user missing on deactivate; tombstoning", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
				return e.store.Save(ctx, action.User.ID, tombstone)
			default:
				e.logger.Error("MAS deactivate failed", "username", action.User.Username, "mas_id", action.ExistingMAS, "status", respErr.Status, "status_code", respErr.StatusCode, "response_body", respErr.Body, "mas_profile_id", action.ProfileID)
				saveErr := e.store.Save(ctx, action.User.ID, state.Entry{
					MASID:       action.ExistingMAS,
					ProfileID:   action.ProfileID,
					Status:      state.StatusError,
					LastError:   fmt.Sprintf("%s: %s", respErr.Status, respErr.Body),
					Username:    action.User.Username,
					LastApplied: action.StateLastApplied,
				})
				if saveErr != nil {
					return fmt.Errorf("record deactivate failure: %w", saveErr)
				}
				return nil
			}
		}
		return fmt.Errorf("deactivate MAS user: %w", err)
	}
	e.logger.Info("deactivated MAS user", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
	return e.store.Save(ctx, action.User.ID, tombstone)
}

// deactivatedSnapshot flips active on the recorded snapshot. A zero snapshot
// stays zero so a later reactivation falls back to a full PUT rather than
// patching against fabricated field state.
func deactivatedSnapshot(prev state.Snapshot) state.Snapshot {
	if prev.IsZero() {
		return prev
	}
	prev.Active = false
	prev.HasActive = true
	return prev
}

func (e *Executor) handleConflict(ctx context.Context, action Action, payload mas.UserResource) error {
	filter := fmt.Sprintf(`externalId eq "%s"`, action.User.ID)
	users, err := e.mas.SearchUsers(ctx, action.ProfileID, filter)
	if err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		// Attempt fallback by username if externalId search failed (e.g., unsupported)
		e.logger.Error("MAS search by externalId failed, retrying by username", "error", err, "username", action.User.Username, "mas_profile_id", action.ProfileID)
		filter = fmt.Sprintf(`userName eq "%s"`, action.User.Username)
		users, err = e.mas.SearchUsers(ctx, action.ProfileID, filter)
		if err != nil {
			if isMASUnauthorizedResponse(err) {
				return wrapMASUnauthorized(err)
			}
			e.logger.Error("MAS search failed after conflict", "username", action.User.Username, "id", action.User.ID, "mas_profile_id", action.ProfileID, "error", err)
			return nil
		}
	}

	if len(users) == 0 {
		// Some MAS profiles accept externalId filters but return empty even when a username match exists.
		filter = fmt.Sprintf(`userName eq "%s"`, action.User.Username)
		fallbackUsers, fallbackErr := e.mas.SearchUsers(ctx, action.ProfileID, filter)
		if fallbackErr != nil {
			if isMASUnauthorizedResponse(fallbackErr) {
				return wrapMASUnauthorized(fallbackErr)
			}
			e.logger.Error("MAS search by username failed after conflict", "username", action.User.Username, "id", action.User.ID, "mas_profile_id", action.ProfileID, "error", fallbackErr)
		} else {
			users = fallbackUsers
		}
	}

	switch len(users) {
	case 1:
		masID := users[0].ID
		e.logger.Info("adopt existing MAS user after conflict",
			"username", action.User.Username,
			"id", action.User.ID,
			"mas_id", masID,
			"mas_profile_id", action.ProfileID,
			"mas_profile_label", action.ProfileLabel,
			"filter", filter,
		)
		if err := e.store.Save(ctx, action.User.ID, state.Entry{
			MASID:       masID,
			ProfileID:   action.ProfileID,
			Status:      state.StatusOK,
			Username:    action.User.Username,
			LastApplied: snapshotFromPayload(payload, action.ProfileID),
		}); err != nil {
			return fmt.Errorf("store adopted correlation: %w", err)
		}
	case 0:
		msg := fmt.Sprintf("no MAS user found for filter %s", filter)
		e.logger.Error("MAS conflict but search returned zero results", "username", action.User.Username, "id", action.User.ID, "mas_profile_id", action.ProfileID, "filter", filter)
		_ = e.store.Save(ctx, action.User.ID, state.Entry{
			MASID:     "",
			ProfileID: action.ProfileID,
			Status:    state.StatusError,
			LastError: msg,
			Username:  action.User.Username,
		})
	default:
		msg := fmt.Sprintf("multiple MAS users matched filter %s (count=%d)", filter, len(users))
		e.logger.Error("MAS conflict with multiple matches", "username", action.User.Username, "id", action.User.ID, "mas_profile_id", action.ProfileID, "filter", filter, "count", len(users))
		_ = e.store.Save(ctx, action.User.ID, state.Entry{
			MASID:     "",
			ProfileID: action.ProfileID,
			Status:    state.StatusError,
			LastError: msg,
			Username:  action.User.Username,
		})
	}
	return nil
}

func (e *Executor) updateWithPatch(ctx context.Context, action Action, payload mas.UserResource) error {
	prevSnapshot := action.StateLastApplied
	usePatch := !prevSnapshot.IsZero()
	if !usePatch {
		return e.performPut(ctx, action, payload)
	}

	nextSnapshot, ops := diffToPatch(prevSnapshot, payload, action.ProfileID)
	if len(ops) == 0 {
		if action.StateStatus == state.StatusDeactivated {
			// Re-added user already matches desired state (e.g. disabled in
			// Keycloak); clear the tombstone so removal detection tracks it again.
			return e.store.Save(ctx, action.User.ID, state.Entry{
				MASID:       action.ExistingMAS,
				ProfileID:   action.ProfileID,
				Status:      state.StatusOK,
				Username:    action.User.Username,
				LastApplied: prevSnapshot,
			})
		}
		// No changes; skip update.
		return nil
	}
	if err := e.mas.PatchUser(ctx, action.ProfileID, action.ExistingMAS, ops); err != nil {
		var respErr *mas.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == 404 {
			// Resource vanished; reapply full document via PUT.
			return e.performPut(ctx, action, payload)
		}
		if errors.As(err, &respErr) {
			if respErr.StatusCode == 401 {
				return wrapMASUnauthorized(err)
			}
			e.logger.Error("MAS patch failed", "username", action.User.Username, "mas_id", action.ExistingMAS, "status", respErr.Status, "status_code", respErr.StatusCode, "response_body", respErr.Body, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
			saveErr := e.store.Save(ctx, action.User.ID, state.Entry{
				MASID:       action.ExistingMAS,
				ProfileID:   action.ProfileID,
				Status:      state.StatusError,
				LastError:   fmt.Sprintf("%s: %s", respErr.Status, respErr.Body),
				Username:    action.User.Username,
				LastApplied: prevSnapshot,
			})
			if saveErr != nil {
				return fmt.Errorf("record patch failure: %w", saveErr)
			}
			return nil
		}
		return fmt.Errorf("patch MAS user: %w", err)
	}

	return e.store.Save(ctx, action.User.ID, state.Entry{
		MASID:       action.ExistingMAS,
		ProfileID:   action.ProfileID,
		Status:      state.StatusOK,
		Username:    action.User.Username,
		LastApplied: nextSnapshot,
	})
}

func (e *Executor) performPut(ctx context.Context, action Action, payload mas.UserResource) error {
	if err := e.mas.UpdateUser(ctx, action.ProfileID, action.ExistingMAS, payload); err != nil {
		var respErr *mas.ResponseError
		if errors.As(err, &respErr) {
			if respErr.StatusCode == 401 {
				return wrapMASUnauthorized(err)
			}
			e.logger.Error("MAS update failed", "username", action.User.Username, "mas_id", action.ExistingMAS, "status", respErr.Status, "status_code", respErr.StatusCode, "response_body", respErr.Body, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
			saveErr := e.store.Save(ctx, action.User.ID, state.Entry{
				MASID:       action.ExistingMAS,
				ProfileID:   action.ProfileID,
				Status:      state.StatusError,
				LastError:   fmt.Sprintf("%s: %s", respErr.Status, respErr.Body),
				Username:    action.User.Username,
				LastApplied: action.StateLastApplied,
			})
			if saveErr != nil {
				return fmt.Errorf("record update failure: %w", saveErr)
			}
			return nil
		}
		return fmt.Errorf("update MAS user: %w", err)
	}
	return e.store.Save(ctx, action.User.ID, state.Entry{
		MASID:       action.ExistingMAS,
		ProfileID:   action.ProfileID,
		Status:      state.StatusOK,
		Username:    action.User.Username,
		LastApplied: snapshotFromPayload(payload, action.ProfileID),
	})
}

func isMASUnauthorizedResponse(err error) bool {
	var respErr *mas.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == 401
}

func wrapMASUnauthorized(err error) error {
	return fmt.Errorf("%w: %v", ErrMASUnauthorized, err)
}
