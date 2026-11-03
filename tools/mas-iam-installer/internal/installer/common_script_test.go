package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Tests for shell helpers in scripts/_all_in_one_common.sh, run under bash
// against a fake `oc` placed first on PATH.

func commonScriptPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "scripts", "_all_in_one_common.sh")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("scripts/_all_in_one_common.sh not found: %v", err)
	}
	return path
}

// fakeOC answers the queries the helpers make. Job conditions advance one
// comma-separated entry of $FAKE_SEQ per poll (the last entry repeats).
const fakeOC = `#!/usr/bin/env bash
args="$*"
case "$args" in
  *'lastObservedState'*)
    printf '%s' "${FAKE_CATALOG_STATE:-}" ;;
  *'get catalogsource'*'.spec.image'*)
    printf 'quay.io/example/catalog:missing' ;;
  *'olm.catalogSource='*)
    printf '%b' "${FAKE_CATALOG_PODS:-}" ;;
  *'{.status.currentCSV}'*)
    if [[ -f "$FAKE_APPLIED" ]]; then printf '%s' "${FAKE_NEW_CSV:-}"; else printf '%s' "${FAKE_OLD_CSV:-}"; fi ;;
  *'{.spec.channel}'*)
    printf 'alpha' ;;
  *'packagemanifests'*)
    printf '%s' "${FAKE_HEAD_CSV:-}" ;;
  "get csv -n "*"-l "*)
    printf '%s\n' "${FAKE_OLD_CSV:-}" ;;
  "get csv -n "*'{.status.phase}'*)
    printf 'Succeeded' ;;
  "apply -f "*)
    touch "$FAKE_APPLIED"
    echo "$args" >>"$FAKE_DELETES" ;;
  "delete subscription"*|"delete csv"*)
    echo "$args" >>"$FAKE_DELETES" ;;
  *'creationTimestamp'*)
    printf '%s' "${FAKE_CREATED:-2099-01-01T00:00:00Z}" ;;
  "get job "*'{.reason}'*)
    printf 'Failed: BackoffLimitExceeded - Job has reached the specified backoff limit\n' ;;
  "get job "*'.status.conditions'*)
    n=$(cat "$FAKE_COUNTER" 2>/dev/null || echo 0)
    echo $((n + 1)) >"$FAKE_COUNTER"
    IFS=',' read -r -a seq <<<"${FAKE_SEQ:-}"
    idx=$n
    if (( ${#seq[@]} == 0 )); then exit 0; fi
    if (( idx >= ${#seq[@]} )); then idx=$(( ${#seq[@]} - 1 )); fi
    [[ -n "${seq[$idx]}" ]] && printf '%s\n' "${seq[$idx]}"
    exit 0 ;;
  "get pods "*"-o name"*)
    echo "pod/myjob-abc12" ;;
  "get pods "*'phase='*)
    echo "myjob-abc12 phase=Failed  Error exit=1" ;;
  "get pods "*'.state.waiting.reason}'*)
    [[ -n "${FAKE_WAITING:-}" ]] && echo "${FAKE_WAITING}"
    exit 0 ;;
  "logs "*)
    echo "FAKE POD LOG: connection refused" ;;
  "delete job "*)
    echo "$args" >>"$FAKE_DELETES" ;;
  *)
    echo "fake oc: unhandled: $args" >&2
    exit 3 ;;
esac
`

type shellRun struct {
	out     string
	err     error
	deletes string
}

func runCommonHelper(t *testing.T, snippet string, env map[string]string) shellRun {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "oc"), []byte(fakeOC), 0o755); err != nil {
		t.Fatal(err)
	}
	deletes := filepath.Join(dir, "deletes")
	script := "set -euo pipefail\nsource " + commonScriptPath(t) + "\n" + snippet + "\n"
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_COUNTER="+filepath.Join(dir, "counter"),
		"FAKE_DELETES="+deletes,
		"FAKE_APPLIED="+filepath.Join(dir, "applied"),
		"MAS_EST_CATALOG_POLL_INTERVAL=1",
		"MAS_EST_CATALOG_FAILURE_GRACE_SECONDS=2",
		"MAS_EST_CSV_POLL_INTERVAL=1",
		"MAS_EST_STALE_CSV_GRACE_SECONDS=2",
		"MAS_EST_RUN_STARTED_AT=2026-11-04T10:00:00Z",
		"MAS_EST_JOB_POLL_INTERVAL=1",
		"MAS_EST_STALE_JOB_GRACE_SECONDS=2",
		"MAS_EST_IMAGE_PULL_GRACE_SECONDS=2",
	)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	raw, _ := os.ReadFile(deletes)
	return shellRun{out: string(out), err: err, deletes: string(raw)}
}

func TestWaitForJobCompleteSucceeds(t *testing.T) {
	run := runCommonHelper(t, `wait_for_job_complete ns myjob 30`, map[string]string{
		"FAKE_SEQ": ",,Complete",
	})
	if run.err != nil {
		t.Fatalf("expected success: %v\n%s", run.err, run.out)
	}
	if !strings.Contains(run.out, "job/myjob complete") {
		t.Fatalf("missing completion log:\n%s", run.out)
	}
}

func TestWaitForJobCompleteFailsFastWithLogs(t *testing.T) {
	run := runCommonHelper(t, `wait_for_job_complete ns myjob 600`, map[string]string{
		"FAKE_SEQ":     ",Failed",
		"FAKE_CREATED": "2026-11-04T10:05:00Z", // created by this run
	})
	if run.err == nil {
		t.Fatalf("a Failed job must fail the wait:\n%s", run.out)
	}
	for _, want := range []string{"BackoffLimitExceeded", "FAKE POD LOG", "job/myjob in namespace ns failed"} {
		if !strings.Contains(run.out, want) {
			t.Fatalf("output missing %q:\n%s", want, run.out)
		}
	}
}

func TestWaitForJobCompleteStaleFailedJobGivesHint(t *testing.T) {
	run := runCommonHelper(t, `wait_for_job_complete ns myjob 600 "DO THE THING"`, map[string]string{
		"FAKE_SEQ":     "Failed",
		"FAKE_CREATED": "2026-11-03T09:00:00Z", // before this run started
	})
	if run.err == nil {
		t.Fatalf("a stale Failed job must fail after the grace period:\n%s", run.out)
	}
	for _, want := range []string{"from an earlier run", "DO THE THING", "FAKE POD LOG"} {
		if !strings.Contains(run.out, want) {
			t.Fatalf("output missing %q:\n%s", want, run.out)
		}
	}
}

func TestWaitForJobCompleteStaleFailedJobReplaced(t *testing.T) {
	// The earlier run's Failed hook Job disappears (Helm before-hook-creation)
	// and its replacement completes: the wait must follow the new one.
	run := runCommonHelper(t, `wait_for_job_complete ns myjob 30`, map[string]string{
		"FAKE_SEQ":     "Failed,,Complete",
		"FAKE_CREATED": "2026-11-03T09:00:00Z",
	})
	if run.err != nil {
		t.Fatalf("replaced stale job should succeed: %v\n%s", run.err, run.out)
	}
}

func TestWaitForJobCompleteImagePullFailsFast(t *testing.T) {
	run := runCommonHelper(t, `wait_for_job_complete ns myjob 600`, map[string]string{
		"FAKE_SEQ":     "",
		"FAKE_WAITING": "ImagePullBackOff",
	})
	if run.err == nil {
		t.Fatalf("an image that never pulls must fail the wait:\n%s", run.out)
	}
	if !strings.Contains(run.out, "ImagePullBackOff") {
		t.Fatalf("output should name the waiting reason:\n%s", run.out)
	}
}

func TestDeleteJobIfFailed(t *testing.T) {
	run := runCommonHelper(t, `delete_job_if_failed ns myjob`, map[string]string{"FAKE_SEQ": "Failed"})
	if run.err != nil || !strings.Contains(run.deletes, "delete job myjob -n ns") {
		t.Fatalf("failed job not deleted: err=%v deletes=%q\n%s", run.err, run.deletes, run.out)
	}
	run = runCommonHelper(t, `delete_job_if_failed ns myjob`, map[string]string{"FAKE_SEQ": "Complete"})
	if run.err != nil || run.deletes != "" {
		t.Fatalf("completed job must be kept: err=%v deletes=%q", run.err, run.deletes)
	}
}

func TestCSVVersionOlder(t *testing.T) {
	cases := []struct {
		a, b  string
		older bool
	}{
		{"mas-iam-operator.v0.0.14", "mas-iam-operator.v0.0.15", true},
		{"mas-iam-operator.v0.0.9", "mas-iam-operator.v0.0.15", true},
		{"mas-iam-operator.v0.1.0", "mas-iam-operator.v0.0.15", false},
		{"mas-iam-operator.v0.0.15", "mas-iam-operator.v0.0.15", false},
		{"mas-iam-operator.v0.0.15-dev", "mas-iam-operator.v0.0.16", true},
		{"mas-iam-operator.vNEXT", "mas-iam-operator.v0.0.15", false},
	}
	for _, tc := range cases {
		snippet := `if csv_version_older "` + tc.a + `" "` + tc.b + `"; then echo OLDER; else echo NOT; fi`
		run := runCommonHelper(t, snippet, nil)
		got := strings.TrimSpace(run.out) == "OLDER"
		if run.err != nil || got != tc.older {
			t.Errorf("csv_version_older(%s, %s) = %v (err %v, out %q), want %v", tc.a, tc.b, got, run.err, run.out, tc.older)
		}
	}
}

func TestWaitForCatalogSourceReady(t *testing.T) {
	run := runCommonHelper(t, `wait_for_catalog_source_ready mas-iam-operator 30`, map[string]string{"FAKE_CATALOG_STATE": "READY"})
	if run.err != nil {
		t.Fatalf("READY catalog: %v\n%s", run.err, run.out)
	}

	run = runCommonHelper(t, `wait_for_catalog_source_ready mas-iam-operator 300`, map[string]string{
		"FAKE_CATALOG_STATE": "CONNECTING",
		"FAKE_CATALOG_PODS":  `mas-iam-operator-abcde ImagePullBackOff Back-off pulling image "quay.io/example/catalog:missing"\n`,
	})
	if run.err == nil {
		t.Fatalf("image pull failure must fail fast:\n%s", run.out)
	}
	for _, want := range []string{"ImagePullBackOff", "quay.io/example/catalog:missing", "cannot pull"} {
		if !strings.Contains(run.out, want) {
			t.Fatalf("output missing %q:\n%s", want, run.out)
		}
	}

	run = runCommonHelper(t, `wait_for_catalog_source_ready mas-iam-operator 300`, map[string]string{"FAKE_CATALOG_STATE": "TRANSIENT_FAILURE"})
	if run.err == nil || !strings.Contains(run.out, "TRANSIENT_FAILURE") {
		t.Fatalf("persistent TRANSIENT_FAILURE must fail: err=%v\n%s", run.err, run.out)
	}
}

func TestWaitForOperatorCSVReplacesStaleOperator(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "sub.yaml")
	if err := os.WriteFile(manifest, []byte("kind: Subscription\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := runCommonHelper(t, `wait_for_operator_csv_succeeded ns 60 "`+manifest+`"; echo "WAITED=$WAITED_OPERATOR_CSV"`, map[string]string{
		"FAKE_OLD_CSV":  "mas-iam-operator.v0.0.14",
		"FAKE_NEW_CSV":  "mas-iam-operator.v0.0.15",
		"FAKE_HEAD_CSV": "mas-iam-operator.v0.0.15",
	})
	if run.err != nil {
		t.Fatalf("stale operator should be replaced: %v\n%s", run.err, run.out)
	}
	for _, want := range []string{"OLDER than the catalog's current", "WAITED=mas-iam-operator.v0.0.15"} {
		if !strings.Contains(run.out, want) {
			t.Fatalf("output missing %q:\n%s", want, run.out)
		}
	}
	for _, want := range []string{"delete subscription.operators.coreos.com mas-iam-operator -n ns", "delete csv mas-iam-operator.v0.0.14 -n ns", "apply -f " + manifest} {
		if !strings.Contains(run.deletes, want) {
			t.Fatalf("missing %q in oc calls:\n%s", want, run.deletes)
		}
	}
}

func TestWaitForOperatorCSVKeepsNewerOperator(t *testing.T) {
	// packageserver can briefly serve older catalog content: a NEWER
	// installed operator must be accepted, never torn down.
	run := runCommonHelper(t, `wait_for_operator_csv_succeeded ns 30 /dev/null`, map[string]string{
		"FAKE_OLD_CSV":  "mas-iam-operator.v0.0.16",
		"FAKE_HEAD_CSV": "mas-iam-operator.v0.0.15",
	})
	if run.err != nil || run.deletes != "" {
		t.Fatalf("newer operator must be accepted untouched: err=%v deletes=%q\n%s", run.err, run.deletes, run.out)
	}
}
