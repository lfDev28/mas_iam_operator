package app

import (
	"fmt"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

// Readiness after an update.
//
// Every IDPCfg / ObjectStorageCfg mas-est touches on a re-run already carries
// Ready=True from the previous run. Accepting that condition right after the
// PUT/apply reports success before MAS has reconciled anything, so a re-run
// that changes a credential can "succeed" while MAS still runs the old
// config. readinessGate only accepts a Ready condition that provably belongs
// to a reconcile after the update:
//
//   - observedGeneration caught up with metadata.generation (when exposed), or
//   - lastTransitionTime at/after the update time (minus readinessClockSkew), or
//   - Ready=True after we saw it not-Ready at some point since the update.
//
// A Ready whose timestamp predates the update is the ambiguous case: either
// the controller has not picked the change up yet, or the update changed
// nothing and Ready legitimately never flipped (a re-run with identical
// config). Waiting for a transition that never comes would turn every
// idempotent re-run into a timeout, so a stale Ready that stays put for
// staleReadyGrace is accepted with a note. A generation lag is never excused.
var (
	readinessClockSkew       = 5 * time.Second
	staleReadyGrace          = 2 * time.Minute
	readinessPollInterval    = 5 * time.Second
	maxConsecutivePollErrors = 12
)

type readinessGate struct {
	grace       time.Duration
	sawNotReady bool
	staleSince  time.Time
}

func newReadinessGate() *readinessGate {
	return &readinessGate{grace: staleReadyGrace}
}

// observe feeds one classified poll result into the gate. done reports the
// wait is over; note is non-empty when the gate accepted a stale Ready.
func (g *readinessGate) observe(freshness masadmin.Freshness, now time.Time) (done bool, note string) {
	switch freshness {
	case masadmin.NotReady:
		g.sawNotReady = true
		g.staleSince = time.Time{}
		return false, ""
	case masadmin.ReadyFresh:
		return true, ""
	case masadmin.ReadyLagging:
		g.staleSince = time.Time{}
		return false, ""
	default: // ReadyStale, ReadyUnknown
		if g.sawNotReady {
			return true, ""
		}
		if g.staleSince.IsZero() {
			g.staleSince = now
		}
		if now.Sub(g.staleSince) >= g.grace {
			return true, fmt.Sprintf("Ready did not change within %s of the update; assuming MAS had nothing to reconcile", g.grace)
		}
		return false, ""
	}
}

// conditionsFreshness classifies Kubernetes-style CR conditions (IDPCfg,
// ObjectStorageCfg) the same way masadmin.IDPConfigResponse.ReadyAfter does.
func conditionsFreshness(generation int64, conditions []oc.ConditionStatus, since time.Time) masadmin.Freshness {
	for _, condition := range conditions {
		if condition.Type != "Ready" || condition.Status != "True" {
			continue
		}
		return masadmin.ClassifyReady(generation, condition.ObservedGeneration, condition.LastTransitionTime, since, readinessClockSkew)
	}
	return masadmin.NotReady
}
