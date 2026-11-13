package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

// RetryPolicy controls how failed entries are retried across poll cycles.
type RetryPolicy struct {
	// BaseBackoff is the wait after the first failure; it doubles per
	// consecutive failure up to MaxBackoff.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// ParkAfter is how many consecutive permanent (non-retryable 4xx)
	// failures for the same desired state park an entry.
	ParkAfter int
}

// DefaultRetryPolicy retries quickly at first (the next poll cycle with the
// default 5m interval) and settles at one attempt every 30 minutes.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{BaseBackoff: 30 * time.Second, MaxBackoff: 30 * time.Minute, ParkAfter: 3}
}

func (p RetryPolicy) backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := p.BaseBackoff
	for i := 1; i < attempts && d < p.MaxBackoff; i++ {
		d *= 2
	}
	if d > p.MaxBackoff {
		d = p.MaxBackoff
	}
	return d
}

// Executor performs MAS SCIM operations for planned actions and records the
// outcome in the state store.
type Executor struct {
	mas          MASClient
	store        state.Store
	logger       Logger
	dryRun       bool
	allowUpdates bool
	fingerprint  string
	retry        RetryPolicy
	now          func() time.Time
}

// ErrMASUnauthorized indicates MAS returned 401 and the caller may refresh credentials.
var ErrMASUnauthorized = errors.New("mas unauthorized")

// NewExecutor wires a new action executor.
func NewExecutor(masClient MASClient, store state.Store, dryRun bool, allowUpdates bool, logger Logger) *Executor {
	return &Executor{
		mas:          masClient,
		store:        store,
		dryRun:       dryRun,
		allowUpdates: allowUpdates,
		logger:       logger,
		retry:        DefaultRetryPolicy(),
		now:          time.Now,
	}
}

// WithConfigFingerprint sets the bridge-config fingerprint folded into every
// desired-state hash, so a config change re-attempts parked entries.
func (e *Executor) WithConfigFingerprint(fp string) *Executor {
	e.fingerprint = fp
	return e
}

// WithRetryPolicy overrides the default retry policy.
func (e *Executor) WithRetryPolicy(p RetryPolicy) *Executor {
	e.retry = p
	return e
}

// WithClock overrides the clock (tests).
func (e *Executor) WithClock(now func() time.Time) *Executor {
	e.now = now
	return e
}

// SetMASClient swaps the runtime MAS client (used after token refresh).
func (e *Executor) SetMASClient(masClient MASClient) {
	e.mas = masClient
}

// Execute performs one planned action. It returns an error only for
// conditions that should abort the whole cycle (MAS 401, state store
// failures); per-user MAS failures are recorded in state and retried with
// backoff on later cycles.
func (e *Executor) Execute(ctx context.Context, action Action) error {
	payload := mas.BuildUserPayload(action.User)
	hash := desiredHash(e.fingerprint, action, payload)
	if skip, reason := e.gate(action, hash); skip {
		e.logger.Info("skip failed entry until retry is due",
			"reason", reason,
			"action", action.Type,
			"username", action.User.Username,
			"id", action.User.ID,
			"error_class", action.Prior.ErrorClass,
			"attempts", action.Prior.Attempts,
			"next_retry_at", action.Prior.NextRetryAt,
			"last_error", action.Prior.LastError,
		)
		return nil
	}
	switch action.Type {
	case ActionCreate:
		return e.create(ctx, action, payload, hash)
	case ActionUpdate:
		if !hasMASID(action.ExistingMAS) {
			// Never materialized in MAS (failed or dry-run create): create.
			action.Type = ActionCreate
			action.ExistingMAS = ""
			return e.create(ctx, action, payload, hash)
		}
		if !e.allowUpdates {
			e.logger.Info("updates disabled", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
			return nil
		}
		if e.dryRun {
			e.logger.Info("dry-run update", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
			return nil
		}
		return e.updateWithPatch(ctx, action, payload, hash)
	case ActionDeactivate:
		return e.deactivate(ctx, action, hash)
	default:
		e.logger.Info("unhandled action", "action", action.Type)
		return nil
	}
}

// gate decides whether a previously failed entry is due for another attempt.
// A changed desired state (Keycloak user edited or disabled, profile or
// bridge config changed) always goes through at once, even when parked or
// backing off: a user disabled in Keycloak must not wait out a backoff.
func (e *Executor) gate(action Action, hash string) (skip bool, reason string) {
	p := action.Prior
	if !action.HasPrior || p.Status != state.StatusError {
		return false, ""
	}
	if p.DesiredHash != "" && p.DesiredHash != hash {
		return false, ""
	}
	if p.Parked {
		return true, "parked"
	}
	if e.now().Before(p.NextRetryAt) {
		return true, "backoff"
	}
	return false, ""
}

func (e *Executor) create(ctx context.Context, action Action, payload mas.UserResource, hash string) error {
	if e.dryRun {
		// Nothing is persisted in dry-run: a fabricated MAS ID in state would
		// make a later live run skip the real create.
		e.logger.Info("dry-run create",
			"username", action.User.Username,
			"mas_profile_label", action.ProfileLabel,
			"mas_profile_id", action.ProfileID,
		)
		return nil
	}
	base := baseEntry(action)
	base.MASID = ""
	masID, err := e.mas.CreateUser(ctx, action.ProfileID, payload)
	if err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		if isMASStatus(err, 409) {
			return e.handleConflict(ctx, action, payload, hash)
		}
		class, detail := classifyMASError(err)
		e.logger.Error("MAS create failed", "username", action.User.Username, "error_class", class, "error", detail, "mas_profile_id", action.ProfileID, "mas_profile_label", action.ProfileLabel)
		return e.recordFailure(ctx, action, base, hash, class, detail)
	}
	base.MASID = masID
	base.Status = state.StatusOK
	base.CreatedByBridge = true
	base.LastApplied = snapshotFromPayload(payload, action.ProfileID)
	base.ClearRetry()
	if err := e.store.Save(ctx, action.User.ID, base); err != nil {
		return fmt.Errorf("store correlation: %w", err)
	}
	return nil
}

// deactivate sets SCIM active:false on the MAS user (never delete) and
// tombstones the state entry so the deactivation fires once. A failed MAS
// call must not tombstone: the entry keeps a non-deactivated status so a
// later cycle plans the deactivation again (with backoff).
func (e *Executor) deactivate(ctx context.Context, action Action, hash string) error {
	if !hasMASID(action.ExistingMAS) {
		return nil
	}
	if !e.allowUpdates {
		e.logger.Info("updates disabled; skipping deactivate", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
		return nil
	}
	if e.dryRun {
		e.logger.Info("dry-run deactivate", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
		return nil
	}
	base := baseEntry(action)
	tombstone := base
	tombstone.Status = state.StatusDeactivated
	tombstone.LastApplied = deactivatedSnapshot(base.LastApplied)
	tombstone.ClearRetry()

	ops := []mas.PatchOperation{{Op: "replace", Path: "active", Value: false}}
	if err := e.mas.PatchUser(ctx, action.ProfileID, action.ExistingMAS, ops); err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		if isMASStatus(err, 404) {
			// Already gone from MAS; tombstone so we stop retrying.
			e.logger.Info("MAS user missing on deactivate; tombstoning", "username", action.User.Username, "mas_id", action.ExistingMAS, "mas_profile_id", action.ProfileID)
			return e.store.Save(ctx, action.User.ID, tombstone)
		}
		class, detail := classifyMASError(err)
		e.logger.Error("MAS deactivate failed", "username", action.User.Username, "mas_id", action.ExistingMAS, "error_class", class, "error", detail, "mas_profile_id", action.ProfileID)
		return e.recordFailure(ctx, action, base, hash, class, detail)
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

// handleConflict resolves a 409 on create. It adopts the existing MAS user
// only when it is provably ours: its externalId is this Keycloak user's ID,
// or state already correlates this Keycloak user with that MAS ID. A
// same-named MAS user owned by someone else is never taken over; the entry
// is parked as a conflict instead. After adoption the desired payload is
// PUT, so MAS reflects Keycloak rather than whatever the old user held.
func (e *Executor) handleConflict(ctx context.Context, action Action, payload mas.UserResource, hash string) error {
	base := baseEntry(action)
	base.MASID = ""
	kcID := action.User.ID
	username := action.User.Username

	extFilter := fmt.Sprintf(`externalId eq "%s"`, kcID)
	byExt, err := e.mas.SearchUsers(ctx, action.ProfileID, extFilter)
	if err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		e.logger.Warn("MAS search by externalId failed after conflict; trying userName", "username", username, "id", kcID, "mas_profile_id", action.ProfileID, "error", err)
		byExt = nil
	}
	// Some MAS profiles ignore or half-support the externalId filter, so
	// re-check what came back: a result carrying a different externalId or
	// a different userName is not ours, whatever the filter said.
	var owned []mas.User
	for _, u := range byExt {
		extOK := u.ExternalID == "" || strings.EqualFold(u.ExternalID, kcID)
		nameOK := u.UserName == "" || strings.EqualFold(u.UserName, username)
		if extOK && nameOK {
			owned = append(owned, u)
		}
	}
	if len(owned) == 1 {
		return e.adopt(ctx, action, payload, hash, owned[0].ID, true, extFilter)
	}

	nameFilter := fmt.Sprintf(`userName eq "%s"`, username)
	byName, err := e.mas.SearchUsers(ctx, action.ProfileID, nameFilter)
	if err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		_, detail := classifyMASError(err)
		e.logger.Error("MAS search by userName failed after conflict", "username", username, "id", kcID, "mas_profile_id", action.ProfileID, "error", detail)
		return e.recordFailure(ctx, action, base, hash, state.ErrorClassTransient, "409 conflict; search failed: "+detail)
	}
	var matches []mas.User
	for _, u := range byName {
		if u.UserName == "" || strings.EqualFold(u.UserName, username) {
			matches = append(matches, u)
		}
	}
	switch len(matches) {
	case 0:
		msg := fmt.Sprintf("409 conflict but no MAS user found for %s or %s", extFilter, nameFilter)
		e.logger.Error("MAS conflict but search returned zero results", "username", username, "id", kcID, "mas_profile_id", action.ProfileID)
		return e.recordFailure(ctx, action, base, hash, state.ErrorClassPermanent, msg)
	case 1:
		m := matches[0]
		if strings.EqualFold(m.ExternalID, kcID) {
			return e.adopt(ctx, action, payload, hash, m.ID, true, nameFilter)
		}
		if action.HasPrior && action.Prior.MASID != "" && action.Prior.MASID == m.ID {
			return e.adopt(ctx, action, payload, hash, m.ID, action.Prior.CreatedByBridge, nameFilter)
		}
		msg := fmt.Sprintf("MAS user %q (id=%s, externalId=%q) already exists and is not owned by this bridge; not adopting", username, m.ID, m.ExternalID)
		e.logger.Error("MAS user conflict: existing user is not bridge-owned; parking entry (resolve in MAS, then run scim-bridge-cleanup --reset-state)",
			"username", username, "id", kcID, "mas_id", m.ID, "mas_external_id", m.ExternalID, "mas_profile_id", action.ProfileID)
		return e.recordFailure(ctx, action, base, hash, state.ErrorClassConflict, msg)
	default:
		msg := fmt.Sprintf("multiple MAS users matched %s (count=%d)", nameFilter, len(matches))
		e.logger.Error("MAS conflict with multiple matches", "username", username, "id", kcID, "mas_profile_id", action.ProfileID, "count", len(matches))
		return e.recordFailure(ctx, action, base, hash, state.ErrorClassPermanent, msg)
	}
}

func (e *Executor) adopt(ctx context.Context, action Action, payload mas.UserResource, hash, masID string, createdByBridge bool, filter string) error {
	e.logger.Info("adopt existing MAS user after conflict",
		"username", action.User.Username,
		"id", action.User.ID,
		"mas_id", masID,
		"mas_profile_id", action.ProfileID,
		"mas_profile_label", action.ProfileLabel,
		"filter", filter,
	)
	action.ExistingMAS = masID
	action.Prior.MASID = masID
	action.Prior.CreatedByBridge = createdByBridge
	// The adopted user's MAS-side fields are unknown: never diff against a
	// snapshot; send the whole desired document.
	action.Prior.LastApplied = state.Snapshot{}
	action.StateLastApplied = state.Snapshot{}
	if !e.allowUpdates {
		// Record the correlation only. The empty snapshot makes the first
		// update after updates are enabled a full PUT.
		entry := baseEntry(action)
		entry.Status = state.StatusOK
		entry.ClearRetry()
		e.logger.Info("updates disabled; adopted without pushing desired state", "username", action.User.Username, "mas_id", masID)
		return e.store.Save(ctx, action.User.ID, entry)
	}
	return e.performPut(ctx, action, payload, hash)
}

func (e *Executor) updateWithPatch(ctx context.Context, action Action, payload mas.UserResource, hash string) error {
	base := baseEntry(action)
	prevSnapshot := base.LastApplied
	if prevSnapshot.IsZero() {
		return e.performPut(ctx, action, payload, hash)
	}

	nextSnapshot, ops := diffToPatch(prevSnapshot, payload, action.ProfileID)
	if len(ops) == 0 {
		if base.Status != state.StatusOK {
			// Desired state already applied (e.g. a re-added user already
			// disabled in Keycloak, or a failure that cleared itself): mark
			// the entry healthy so removal detection and retries reset.
			base.Status = state.StatusOK
			base.ClearRetry()
			return e.store.Save(ctx, action.User.ID, base)
		}
		return nil
	}
	if err := e.mas.PatchUser(ctx, action.ProfileID, action.ExistingMAS, ops); err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		if isMASStatus(err, 404) {
			// Resource vanished; reapply full document via PUT.
			return e.performPut(ctx, action, payload, hash)
		}
		class, detail := classifyMASError(err)
		e.logger.Error("MAS patch failed", "username", action.User.Username, "mas_id", action.ExistingMAS, "error_class", class, "error", detail, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
		return e.recordFailure(ctx, action, base, hash, class, detail)
	}
	paths := make([]string, 0, len(ops))
	for _, op := range ops {
		paths = append(paths, op.Op+" "+op.Path)
	}
	e.logger.Info("MAS user updated", "username", action.User.Username, "mas_id", action.ExistingMAS, "changes", strings.Join(paths, ", "))
	base.Status = state.StatusOK
	base.LastApplied = nextSnapshot
	base.ClearRetry()
	return e.store.Save(ctx, action.User.ID, base)
}

func (e *Executor) performPut(ctx context.Context, action Action, payload mas.UserResource, hash string) error {
	base := baseEntry(action)
	if err := e.mas.UpdateUser(ctx, action.ProfileID, action.ExistingMAS, payload); err != nil {
		if isMASUnauthorizedResponse(err) {
			return wrapMASUnauthorized(err)
		}
		class, detail := classifyMASError(err)
		e.logger.Error("MAS update failed", "username", action.User.Username, "mas_id", action.ExistingMAS, "error_class", class, "error", detail, "mas_profile_label", action.ProfileLabel, "mas_profile_id", action.ProfileID)
		return e.recordFailure(ctx, action, base, hash, class, detail)
	}
	base.Status = state.StatusOK
	base.LastApplied = snapshotFromPayload(payload, action.ProfileID)
	base.ClearRetry()
	return e.store.Save(ctx, action.User.ID, base)
}

// recordFailure stores a failed attempt with backoff bookkeeping. Attempts
// count consecutive failures of the same class for the same desired state;
// permanent failures park after RetryPolicy.ParkAfter of them, conflicts
// park at once, transient failures never park.
func (e *Executor) recordFailure(ctx context.Context, action Action, entry state.Entry, hash, class, detail string) error {
	if err := ctx.Err(); err != nil {
		// Shutting down mid-call: not a MAS failure, leave state untouched.
		return err
	}
	attempts := 1
	p := action.Prior
	if action.HasPrior && p.Status == state.StatusError && p.ErrorClass == class && p.DesiredHash == hash {
		attempts = p.Attempts + 1
	}
	entry.Status = state.StatusError
	entry.LastError = detail
	entry.ErrorClass = class
	entry.Attempts = attempts
	entry.DesiredHash = hash
	switch class {
	case state.ErrorClassConflict:
		entry.Parked = true
	case state.ErrorClassPermanent:
		entry.Parked = e.retry.ParkAfter > 0 && attempts >= e.retry.ParkAfter
	default:
		entry.Parked = false
	}
	entry.NextRetryAt = e.now().Add(e.retry.backoff(attempts))
	if entry.Parked {
		e.logger.Error("parking entry: not retrying until the Keycloak user, MAS profile or bridge config changes (or scim-bridge-cleanup --reset-state)",
			"username", action.User.Username, "id", action.User.ID, "error_class", class, "attempts", attempts, "last_error", detail)
	} else {
		e.logger.Warn("MAS operation failed; will retry",
			"username", action.User.Username, "id", action.User.ID, "error_class", class, "attempts", attempts, "next_retry_at", entry.NextRetryAt.Format(time.RFC3339))
	}
	if err := e.store.Save(ctx, action.User.ID, entry); err != nil {
		return fmt.Errorf("record MAS failure: %w", err)
	}
	return nil
}

// baseEntry is the state entry an outcome is written onto: the prior entry
// (keeping MAS ID, snapshot and ownership) with the action's current view.
func baseEntry(action Action) state.Entry {
	entry := action.Prior
	entry.ProfileID = action.ProfileID
	if action.User.Username != "" {
		entry.Username = action.User.Username
	}
	if action.ExistingMAS != "" {
		entry.MASID = action.ExistingMAS
	}
	if entry.LastApplied.IsZero() {
		entry.LastApplied = action.StateLastApplied
	}
	if entry.Status == "" {
		entry.Status = action.StateStatus
	}
	return entry
}

// classifyMASError maps a MAS failure to an error class. 409 and 401 are
// handled by callers before this is reached.
func classifyMASError(err error) (class, detail string) {
	var respErr *mas.ResponseError
	if errors.As(err, &respErr) {
		detail = fmt.Sprintf("%s: %s", respErr.Status, respErr.Body)
		switch respErr.StatusCode {
		case 400, 404, 409, 422:
			return state.ErrorClassPermanent, detail
		default:
			return state.ErrorClassTransient, detail
		}
	}
	return state.ErrorClassTransient, err.Error()
}

func hasMASID(id string) bool {
	return id != "" && !strings.HasPrefix(id, state.DryRunIDPrefix)
}

func isMASStatus(err error, code int) bool {
	var respErr *mas.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == code
}

func isMASUnauthorizedResponse(err error) bool {
	return isMASStatus(err, 401)
}

func wrapMASUnauthorized(err error) error {
	return fmt.Errorf("%w: %v", ErrMASUnauthorized, err)
}
