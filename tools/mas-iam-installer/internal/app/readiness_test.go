package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func fastReadiness(t *testing.T, grace time.Duration) {
	t.Helper()
	oldPoll, oldGrace, oldMax := readinessPollInterval, staleReadyGrace, maxConsecutivePollErrors
	readinessPollInterval = time.Millisecond
	staleReadyGrace = grace
	maxConsecutivePollErrors = 4
	t.Cleanup(func() {
		readinessPollInterval, staleReadyGrace, maxConsecutivePollErrors = oldPoll, oldGrace, oldMax
	})
}

func idpResponse(status, lastTransition string) *masadmin.IDPConfigResponse {
	resp := &masadmin.IDPConfigResponse{}
	if status != "" {
		resp.Status.Conditions = []masadmin.IDPCondition{{Type: "Ready", Status: status, LastTransitionTime: lastTransition}}
	}
	return resp
}

func TestClassifyReady(t *testing.T) {
	put := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	skew := 5 * time.Second
	cases := []struct {
		name                 string
		generation, observed int64
		ltt                  string
		want                 masadmin.Freshness
	}{
		{"generation caught up", 3, 3, "2020-01-01T00:00:00Z", masadmin.ReadyFresh},
		{"generation lagging ignores a recent timestamp", 4, 3, "2026-11-04T10:05:00Z", masadmin.ReadyLagging},
		{"transition after put", 0, 0, "2026-11-04T10:00:30Z", masadmin.ReadyFresh},
		{"transition within clock skew", 0, 0, "2026-11-04T09:59:57Z", masadmin.ReadyFresh},
		{"transition before put", 0, 0, "2026-11-04T09:30:00Z", masadmin.ReadyStale},
		{"no timestamp", 0, 0, "", masadmin.ReadyUnknown},
		{"garbage timestamp", 0, 0, "yesterday", masadmin.ReadyUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := masadmin.ClassifyReady(tc.generation, tc.observed, tc.ltt, put, skew); got != tc.want {
				t.Fatalf("ClassifyReady = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadinessGate(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

	t.Run("stale ready is not accepted before the grace period", func(t *testing.T) {
		g := &readinessGate{grace: time.Minute}
		if done, _ := g.observe(masadmin.ReadyStale, now); done {
			t.Fatal("stale Ready accepted immediately")
		}
		if done, _ := g.observe(masadmin.ReadyStale, now.Add(30*time.Second)); done {
			t.Fatal("stale Ready accepted before grace")
		}
		done, note := g.observe(masadmin.ReadyStale, now.Add(time.Minute))
		if !done || note == "" {
			t.Fatalf("stale Ready after grace: done=%v note=%q, want accepted with a note", done, note)
		}
	})

	t.Run("ready after a not-ready observation is accepted", func(t *testing.T) {
		g := &readinessGate{grace: time.Hour}
		g.observe(masadmin.NotReady, now)
		if done, note := g.observe(masadmin.ReadyStale, now.Add(time.Second)); !done || note != "" {
			t.Fatalf("done=%v note=%q, want accepted without note", done, note)
		}
	})

	t.Run("generation lag is never excused by the grace period", func(t *testing.T) {
		g := &readinessGate{grace: time.Second}
		for i := 0; i < 5; i++ {
			if done, _ := g.observe(masadmin.ReadyLagging, now.Add(time.Duration(i)*time.Hour)); done {
				t.Fatal("lagging generation accepted")
			}
		}
	})

	t.Run("fresh ready is accepted at once", func(t *testing.T) {
		g := &readinessGate{grace: time.Hour}
		if done, _ := g.observe(masadmin.ReadyFresh, now); !done {
			t.Fatal("fresh Ready not accepted")
		}
	})
}

func TestWaitForAdminAPIReadyIgnoresStaleInitialReady(t *testing.T) {
	fastReadiness(t, time.Hour)
	put := time.Now()
	stale := put.Add(-time.Hour).UTC().Format(time.RFC3339)
	fresh := put.Add(time.Minute).UTC().Format(time.RFC3339)

	polls := 0
	poll := func() (*masadmin.IDPConfigResponse, error) {
		polls++
		if polls < 3 {
			return idpResponse("True", stale), nil
		}
		return idpResponse("True", fresh), nil
	}
	if err := waitForAdminAPIReady(context.Background(), "oidc/default", put, idpResponse("True", stale), time.Minute, poll); err != nil {
		t.Fatalf("waitForAdminAPIReady: %v", err)
	}
	if polls != 3 {
		t.Fatalf("polls = %d, want 3 (the stale initial Ready and the first two stale polls must not end the wait)", polls)
	}
}

func TestWaitForAdminAPIReadyAcceptsUnchangedReadyAfterGrace(t *testing.T) {
	fastReadiness(t, 20*time.Millisecond)
	put := time.Now()
	stale := put.Add(-time.Hour).UTC().Format(time.RFC3339)
	poll := func() (*masadmin.IDPConfigResponse, error) { return idpResponse("True", stale), nil }
	if err := waitForAdminAPIReady(context.Background(), "ldap/default", put, idpResponse("True", stale), time.Minute, poll); err != nil {
		t.Fatalf("an unchanged Ready must be accepted after the grace period: %v", err)
	}
}

func TestWaitForAdminAPIReadyUsesServerTime(t *testing.T) {
	fastReadiness(t, time.Hour)
	// Laptop clock one hour ahead of the cluster: a transition that is fresh
	// on the cluster clock looks an hour old against the local PUT time.
	clusterNow := time.Now().Add(-time.Hour)
	initial := idpResponse("True", clusterNow.Add(2*time.Second).UTC().Format(time.RFC3339))
	initial.ServerTime = clusterNow
	poll := func() (*masadmin.IDPConfigResponse, error) {
		t.Fatal("must not poll: the initial response is fresh by the server clock")
		return nil, nil
	}
	if err := waitForAdminAPIReady(context.Background(), "saml/default", time.Now(), initial, time.Minute, poll); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForAdminAPIReadyToleratesTransientPollErrors(t *testing.T) {
	fastReadiness(t, time.Hour)
	put := time.Now()
	fresh := put.Add(time.Minute).UTC().Format(time.RFC3339)
	polls := 0
	poll := func() (*masadmin.IDPConfigResponse, error) {
		polls++
		if polls <= 2 {
			return nil, errors.New("dial tcp: connection reset")
		}
		return idpResponse("True", fresh), nil
	}
	if err := waitForAdminAPIReady(context.Background(), "oidc/default", put, nil, time.Minute, poll); err != nil {
		t.Fatalf("two poll failures must not abort the wait: %v", err)
	}
}

func TestWaitForAdminAPIReadyAbortsOnPersistentPollErrors(t *testing.T) {
	fastReadiness(t, time.Hour)
	polls := 0
	poll := func() (*masadmin.IDPConfigResponse, error) {
		polls++
		return nil, errors.New("HTTP 403 forbidden")
	}
	err := waitForAdminAPIReady(context.Background(), "oidc/default", time.Now(), nil, time.Minute, poll)
	if err == nil || !strings.Contains(err.Error(), "consecutive failures") {
		t.Fatalf("err = %v, want consecutive-failure abort", err)
	}
	if polls != maxConsecutivePollErrors {
		t.Fatalf("polls = %d, want %d", polls, maxConsecutivePollErrors)
	}
}

func TestJobOutcome(t *testing.T) {
	if done, err := jobOutcome("ns", oc.JobStatus{Name: "j", Active: 1}); done || err != nil {
		t.Fatalf("running job: done=%v err=%v", done, err)
	}
	if done, err := jobOutcome("ns", oc.JobStatus{Name: "j", Complete: true}); !done || err != nil {
		t.Fatalf("complete job: done=%v err=%v", done, err)
	}
	done, err := jobOutcome("ns", oc.JobStatus{Name: "j", FailedCondition: true, FailedReason: "BackoffLimitExceeded", FailedMessage: "too many retries"})
	if !done || err == nil || !strings.Contains(err.Error(), "BackoffLimitExceeded") {
		t.Fatalf("failed job: done=%v err=%v", done, err)
	}
}

func TestConditionsFreshness(t *testing.T) {
	since := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	raw := []byte(`{"metadata":{"generation":2},"status":{"conditions":[
		{"type":"Running","status":"True","lastTransitionTime":"2026-11-04T10:01:00Z"},
		{"type":"Ready","status":"True","lastTransitionTime":"2026-11-04T09:00:00Z"}]}}`)
	generation, conditions, err := oc.ParseResourceConditions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := conditionsFreshness(generation, conditions, since); got != masadmin.ReadyStale {
		t.Fatalf("freshness = %v, want ReadyStale (Ready predates the apply)", got)
	}

	raw = []byte(`{"metadata":{"generation":2},"status":{"observedGeneration":2,"conditions":[
		{"type":"Ready","status":"True","lastTransitionTime":"2026-11-04T09:00:00Z"}]}}`)
	generation, conditions, err = oc.ParseResourceConditions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := conditionsFreshness(generation, conditions, since); got != masadmin.ReadyFresh {
		t.Fatalf("freshness = %v, want ReadyFresh (status.observedGeneration caught up)", got)
	}

	if got := conditionsFreshness(1, []oc.ConditionStatus{{Type: "Ready", Status: "False"}}, since); got != masadmin.NotReady {
		t.Fatalf("freshness = %v, want NotReady", got)
	}
}
