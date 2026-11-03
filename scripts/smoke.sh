#!/usr/bin/env bash
# Cluster smoke test for a mas-est CLI image: a fresh install, and an upgrade
# from the previous release, each into a throwaway namespace.
#
# Usage:
#   scripts/smoke.sh --image <tag> [options] [-- <extra mas-est install flags>]
#   make smoke SMOKE_ARGS="--image v0.1.16-rc.1 --leg both --yes"
#
#   --image <tag|ref>      candidate CLI image (required), e.g. v0.1.16-rc.1 or a full ref
#   --previous <tag|ref>   release to upgrade from (default: highest vX.Y.Z git tag below the candidate)
#   --leg fresh|upgrade|both   (default: both)
#   --namespace <ns>       throwaway namespace (default: mas-est-smoke). Never point this at a real install.
#   --components <list>    (default: ldap,keycloak,s3,smtp). scim and --configure-mas-auth change
#                          shared MAS state (SCIM profile, IDPCfgs): add them only on a disposable MAS,
#                          with their flags after --.
#   --source-ref <git-ref> source the candidate was built from, for the RBAC comparison (default: HEAD)
#   --keep                 keep the namespace after a green run (it is always kept after a red one)
#   --yes                  do not ask before touching the cluster
#   --dry-run              print every command instead of running it; touches nothing
#
# Each leg asserts, after `mas-est install` returns:
#   1. the install Job ran the candidate image (the in-cluster Job image is
#      derived from the CLI's version, so a tag/version skew shows up here)
#   2. every non-Job pod in the namespace is Ready
#   3. `mas-est status` is green for the selected components
#   4. the installed CSV == the packagemanifest currentCSV == the operator
#      version pinned in the source
#   5. the live installer ClusterRole and Role rules equal what the candidate
#      CLI renders (RBAC is applied by the launching CLI, not the Job image)
# The upgrade leg installs the previous release, checks it, then installs the
# candidate over it without wiping and checks again. Testing only fresh
# installs hides upgrade bugs (survivor ClusterRoles, SCCs, CRDs, PVCs).
#
# Cluster-wide side effects, even in a throwaway namespace: the install
# applies the CatalogSource openshift-marketplace/mas-iam-operator (other
# mas-est namespaces on the cluster will see the candidate's catalog) and
# the shared ClusterRole mas-est-installer. Use a scratch cluster.
#
# Needs: oc (logged in), podman or docker, jq, go (for the RBAC render), git.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

CLI_REPO=quay.io/lee_forster/mas-external-services-tool
usage() { sed -n '2,41p' "$0" | sed 's/^# \{0,1\}//'; }

image=""; previous=""; leg=both; namespace=mas-est-smoke
components=ldap,keycloak,s3,smtp; source_ref=HEAD
keep=false; yes=false; dry_run=false; extra=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --image) image="${2:-}"; shift 2 ;;
    --previous) previous="${2:-}"; shift 2 ;;
    --leg) leg="${2:-}"; shift 2 ;;
    --namespace) namespace="${2:-}"; shift 2 ;;
    --components) components="${2:-}"; shift 2 ;;
    --source-ref) source_ref="${2:-}"; shift 2 ;;
    --keep) keep=true; shift ;;
    --yes|-y) yes=true; shift ;;
    --dry-run) dry_run=true; shift ;;
    -h|--help) usage; exit 0 ;;
    --) shift; extra=("$@"); break ;;
    *) echo "error: unknown argument: $1 (install flags go after --)" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "${image}" ]] || { usage >&2; echo >&2; echo "error: --image is required" >&2; exit 2; }
case "${leg}" in fresh|upgrade|both) ;; *) echo "error: --leg must be fresh, upgrade or both" >&2; exit 2 ;; esac
case "${namespace}" in
  mas-est|mas-*-core|openshift-*|kube-*|default) echo "error: refusing to smoke-test in namespace ${namespace}" >&2; exit 2 ;;
esac

as_ref() { [[ "$1" == */* ]] && echo "$1" || echo "${CLI_REPO}:$1"; }
image="$(as_ref "${image}")"
candidate_tag="${image##*:}"

if [[ "${leg}" != fresh && -z "${previous}" ]]; then
  core="${candidate_tag#v}"; core="${core%%-*}"
  previous="$(git tag -l 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed 's/^v//' \
    | awk -v c="${core}" 'BEGIN { split(c, cv, ".") }
        { split($0, v, ".")
          if (v[1] < cv[1] || (v[1] == cv[1] && (v[2] < cv[2] || (v[2] == cv[2] && v[3] < cv[3])))) print }' \
    | sort -t. -k1,1n -k2,2n -k3,3n | tail -1 || true)"
  [[ -n "${previous}" ]] || { echo "error: no previous vX.Y.Z git tag below ${candidate_tag}; pass --previous" >&2; exit 2; }
  previous="v${previous}"
fi
[[ -z "${previous}" ]] || previous="$(as_ref "${previous}")"

ENGINE="${CONTAINER_ENGINE:-}"
if [[ -z "${ENGINE}" ]]; then
  if command -v podman >/dev/null 2>&1; then ENGINE=podman; else ENGINE=docker; fi
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/mas-est-smoke.XXXXXX")"
trap '[[ "${dry_run}" == true ]] && rm -rf "${WORK}"' EXIT

failures=0
step() { echo; echo "==== $*"; }
pass() { echo "PASS  $*"; }
bad()  { echo "FAIL  $*"; failures=$((failures + 1)); }
run() {
  echo "+ $*"
  [[ "${dry_run}" == true ]] || "$@"
}

# ---------------------------------------------------------------------------
preflight() {
  step "smoke: candidate ${image}${previous:+, previous ${previous}}; leg=${leg}; namespace=${namespace}"
  local tool
  for tool in oc jq git go "${ENGINE}"; do
    command -v "${tool}" >/dev/null 2>&1 || { [[ "${dry_run}" == true ]] || { echo "error: ${tool} not found" >&2; exit 1; }; }
  done
  if [[ "${dry_run}" != true ]]; then
    echo "cluster: $(oc whoami) @ $(oc whoami --show-server)"
    if ! ./scripts/check-images.sh --ref "${image}" --archs amd64 >/dev/null; then
      echo "error: ${image} is not on the registry" >&2
      exit 1
    fi
    if [[ "${yes}" != true ]]; then
      echo "This installs into namespace ${namespace} and re-applies the cluster-wide"
      echo "CatalogSource openshift-marketplace/mas-iam-operator and ClusterRole mas-est-installer."
      read -r -p "Continue on this cluster? [y/N] " answer
      [[ "${answer}" == y || "${answer}" == Y ]] || exit 1
    fi
    # The RBAC assertion renders the candidate from --source-ref; make sure
    # that is the commit the image was built from (release builds label it).
    local rev
    rev="$(git rev-parse --short=12 "${source_ref}")"
    if ! ./scripts/check-images.sh --ref "${image}" --label "org.opencontainers.image.revision=${rev}" >/dev/null 2>&1; then
      echo "warning: ${image} is not labelled as built from ${source_ref} (${rev}); the RBAC comparison may use the wrong source (pass --source-ref)"
    fi
  fi
}

# bootstrap <ref> <dir>: extract that image's mas-est launcher into <dir>.
bootstrap() {
  run mkdir -p "$2"
  run "${ENGINE}" run --rm --pull always -v "$2:/tmp" "$1" bootstrap --force
  [[ "${dry_run}" == true ]] && return 0
  local want="${1##*:}" got
  want="${want#v}"
  got="$("$2/mas-est" version | awk 'NR == 1 { print $1 }')"
  if [[ "${got}" == "${want}" ]]; then pass "$1 bootstraps a CLI reporting ${got}"; else
    bad "$1 bootstraps a CLI reporting '${got}', expected ${want} (tag and version.go disagree)"
  fi
}

# mas_est <dir> <ref> args...: run a bootstrapped CLI the way a user would,
# with MAS_EST_IMAGE naming the image (the stale-runtime guard checks it).
mas_est() {
  local dir="$1" ref="$2"; shift 2
  run env MAS_EST_IMAGE="${ref}" "${dir}/mas-est" "$@"
}

wipe_namespace() { # uses the candidate CLI; uninstall only deletes the namespace
  if [[ "${dry_run}" == true ]] || oc get namespace "${namespace}" >/dev/null 2>&1; then
    mas_est "$1" "$2" uninstall --namespace "${namespace}" --non-interactive --skip-profile-delete -y \
      || { echo "error: could not clear namespace ${namespace}" >&2; exit 1; }
  fi
}

install() { # install <dir> <ref> <job-name>
  local -a flags=(--non-interactive --namespace "${namespace}" --components "${components}" --job-name "$3")
  [[ ",${components}," == *,s3,* ]] && flags+=(--skip-s3-mas-config)
  # ${extra[@]+...}: an empty array is "unbound" under set -u in bash 3.2.
  if ! mas_est "$1" "$2" install "${flags[@]}" ${extra[@]+"${extra[@]}"}; then
    bad "mas-est install with $2 failed (job/$3: mas-est logs --namespace ${namespace} --component install-job)"
  fi
}

# ---------------------------------------------------------------------------
# Assertions
assert_job_image() { # <job-name> <expected-ref>
  local got
  got="$(oc -n "${namespace}" get job "$1" -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || true)"
  if [[ "${got}" == "$2" ]]; then pass "job/$1 ran ${got}"; else bad "job/$1 ran '${got}', expected $2 (CLI version / image tag skew)"; fi
}

assert_pods_ready() {
  local not_ready
  not_ready="$(oc -n "${namespace}" get pods -o json | jq -r '
    .items[]
    | select((.metadata.ownerReferences // []) | map(.kind) | index("Job") | not)
    | select(.status.phase != "Succeeded")
    | select(([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) == 0)
    | "\(.metadata.name) phase=\(.status.phase)"')"
  if [[ -z "${not_ready}" ]]; then
    pass "all pods Ready"
  else
    bad "pods not Ready:"; sed 's/^/      /' <<<"${not_ready}"
  fi
  local failed_jobs
  failed_jobs="$(oc -n "${namespace}" get jobs -o json | jq -r '.items[] | select((.status.failed // 0) > 0) | .metadata.name')"
  [[ -z "${failed_jobs}" ]] || { bad "jobs with failed pods:"; sed 's/^/      /' <<<"${failed_jobs}"; }
}

assert_status_green() { # <dir> <ref>
  local out label rc=0 before="${failures}"
  out="$(MAS_EST_IMAGE="$2" "$1/mas-est" status --namespace "${namespace}" 2>&1)" || rc=$?
  echo "${out}" | sed 's/^/      /'
  [[ "${rc}" -eq 0 ]] || bad "mas-est status exited ${rc}"
  grep -q 'error:' <<<"${out}" && bad "mas-est status reports errors"
  grep -qE '^CSV: .*phase=Succeeded' <<<"${out}" || bad "mas-est status: CSV not Succeeded"
  local -a labels=("Operator controller")
  [[ ",${components}," == *,ldap,* ]] && labels+=("OpenLDAP")
  [[ ",${components}," == *,keycloak,* ]] && labels+=("IAM core")
  [[ ",${components}," == *,scim,* ]] && labels+=("SCIM bridge")
  [[ ",${components}," == *,s3,* ]] && labels+=("S3 MinIO")
  [[ ",${components}," == *,smtp,* ]] && labels+=("SMTP Mailpit")
  for label in "${labels[@]}"; do
    # ready=N/N with N >= 1
    if ! grep -E "^${label}: ready=([1-9][0-9]*)/\\1 " <<<"${out}" >/dev/null; then
      bad "mas-est status: ${label} is not ready=N/N"
    fi
  done
  if [[ ",${components}," == *,keycloak,* ]] && ! grep -qE '^PostgreSQL: phase=Running ready=([1-9])/\1' <<<"${out}"; then
    bad "mas-est status: PostgreSQL not ready"
  fi
  grep -qE 'failed=[1-9]' <<<"${out}" && bad "mas-est status: a job has failed pods"
  [[ "${failures}" -gt "${before}" ]] || pass "mas-est status green"
  return 0
}

assert_csv_current() { # [git-ref whose operator pin is expected; empty = skip that part]
  local csvs installed current expected=""
  csvs="$(oc -n "${namespace}" get csv -o json \
    | jq -r '.items[] | select(.metadata.name | startswith("mas-iam-operator.")) | "\(.metadata.name) \(.status.phase)"')"
  installed="$(awk '$2 == "Succeeded" { print $1 }' <<<"${csvs}")"
  if [[ "$(grep -c . <<<"${csvs}")" -ne 1 ]]; then
    bad "expected exactly one mas-iam-operator CSV; found (an unfinished OLM upgrade?):"
    sed 's/^/      /' <<<"${csvs:-none}"
    return 0
  fi
  current="$(oc -n "${namespace}" get packagemanifests -l catalog=mas-iam-operator -o json \
    | jq -r '[.items[] | select(.metadata.name == "mas-iam-operator")][0].status.channels[]? | select(.name == "alpha") | .currentCSV')"
  if [[ -n "${1:-}" ]] && git rev-parse -q --verify "$1^{commit}" >/dev/null; then
    expected="mas-iam-operator.v$(git show "$1:operators/mas-iam-operator/Makefile" | sed -n 's/^VERSION ?= //p')"
  fi
  if [[ -n "${installed}" && "${installed}" == "${current}" && ( -z "${expected}" || "${installed}" == "${expected}" ) ]]; then
    pass "CSV ${installed} Succeeded == packagemanifest currentCSV${expected:+ == pin at $1}"
  else
    bad "CSV installed='${installed}' currentCSV='${current}' expected='${expected:-n/a}' (catalog not published, or OLM did not upgrade)"
  fi
}

# Render the candidate's installer RBAC from its source with a throwaway test
# in a copy of the module (the tree is not modified), then compare with the
# live objects. A mismatch means the launcher that applied the RBAC was not
# the candidate CLI.
render_rbac() {
  local src="${WORK}/src"
  rm -rf "${src}"; mkdir -p "${src}"
  git archive "${source_ref}" tools/mas-iam-installer | tar -x -C "${src}"
  cat >"${src}/tools/mas-iam-installer/internal/app/zz_smoke_rbac_test.go" <<'GO'
package app

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSmokeDumpInstallerRBAC(t *testing.T) {
	out := os.Getenv("SMOKE_RBAC_OUT")
	if out == "" {
		t.Skip("SMOKE_RBAC_OUT not set")
	}
	data, err := json.Marshal(map[string]any{
		"clusterRole": installerClusterRoleManifest()["rules"],
		"role":        installerRoleManifest(os.Getenv("SMOKE_NAMESPACE"))["rules"],
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
GO
  (cd "${src}/tools/mas-iam-installer" && SMOKE_RBAC_OUT="${WORK}/rbac.json" SMOKE_NAMESPACE="${namespace}" \
    go test ./internal/app -run '^TestSmokeDumpInstallerRBAC$' -count=1 >/dev/null)
}

assert_rbac_matches() {
  if ! render_rbac; then
    bad "could not render the candidate's RBAC from ${source_ref}"
    return
  fi
  local want got
  want="$(jq -S '.clusterRole' "${WORK}/rbac.json")"
  got="$(oc get clusterrole mas-est-installer -o json | jq -S '.rules')"
  if [[ "${want}" == "${got}" ]]; then pass "ClusterRole mas-est-installer matches the candidate"; else
    bad "ClusterRole mas-est-installer differs from the candidate's rendering:"
    diff <(echo "${want}") <(echo "${got}") | sed 's/^/      /' || true
  fi
  want="$(jq -S '.role' "${WORK}/rbac.json")"
  got="$(oc -n "${namespace}" get role mas-est-installer -o json | jq -S '.rules')"
  if [[ "${want}" == "${got}" ]]; then pass "Role ${namespace}/mas-est-installer matches the candidate"; else
    bad "Role ${namespace}/mas-est-installer differs from the candidate's rendering:"
    diff <(echo "${want}") <(echo "${got}") | sed 's/^/      /' || true
  fi
}

# assert_all <dir> <ref> <job-name> <check-rbac:true|false> <source-ref for the operator pin>
assert_all() {
  if [[ "${dry_run}" == true ]]; then
    echo "  (dry-run) would assert: job/$3 image == $2; pods Ready; mas-est status green;"
    echo "  (dry-run)   installed CSV == packagemanifest currentCSV == pin at $5"
    [[ "$4" == true ]] && echo "  (dry-run)   live ClusterRole/Role mas-est-installer == candidate rendering from ${source_ref}"
    return 0
  fi
  assert_job_image "$3" "$2"
  assert_pods_ready
  assert_status_green "$1" "$2"
  assert_csv_current "$5"
  [[ "$4" == true ]] && assert_rbac_matches
  return 0
}

# ---------------------------------------------------------------------------
leg_fresh() {
  local dir="${WORK}/candidate"
  step "fresh: install ${image} into an empty ${namespace}"
  bootstrap "${image}" "${dir}"
  wipe_namespace "${dir}" "${image}"
  install "${dir}" "${image}" mas-est-install
  assert_all "${dir}" "${image}" mas-est-install true "${source_ref}"
}

leg_upgrade() {
  local prev_dir="${WORK}/previous" dir="${WORK}/candidate"
  step "upgrade: install ${previous}, then ${image} over it without wiping"
  bootstrap "${previous}" "${prev_dir}"
  bootstrap "${image}" "${dir}"
  wipe_namespace "${dir}" "${image}"
  install "${prev_dir}" "${previous}" mas-est-install
  local before="${failures}"
  # The previous release's RBAC is its own; only check it is healthy.
  assert_all "${prev_dir}" "${previous}" mas-est-install false "${previous##*:}"
  if [[ "${failures}" -gt "${before}" ]]; then
    echo "note  the PREVIOUS release (${previous}) is unhealthy here; candidate results below are still reported"
  fi
  # A finished install Job is kept as the post-mortem record, and the CLI
  # refuses to replace it non-interactively, so the upgrade uses a new name.
  install "${dir}" "${image}" mas-est-install-upgrade
  assert_all "${dir}" "${image}" mas-est-install-upgrade true "${source_ref}"
}

preflight
if [[ "${leg}" == fresh || "${leg}" == both ]]; then leg_fresh; fi
if [[ "${leg}" == upgrade || "${leg}" == both ]]; then leg_upgrade; fi

echo
if [[ "${dry_run}" == true ]]; then
  echo "smoke: dry run only; nothing was run"
  exit 0
fi
if [[ "${failures}" -eq 0 ]]; then
  echo "smoke: GREEN (${leg})"
  if [[ "${keep}" != true ]]; then
    wipe_namespace "${WORK}/candidate" "${image}"
  fi
  rm -rf "${WORK}"
  exit 0
fi
echo "smoke: RED, ${failures} failure(s). Namespace ${namespace} kept for inspection; work dir ${WORK}"
exit 1
