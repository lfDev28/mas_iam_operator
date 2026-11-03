#!/usr/bin/env bash

log_with_prefix() {
  local prefix="$1"
  shift
  printf '[%s] %s\n' "${prefix}" "$*"
}

log_preflight() {
  log_with_prefix preflight "$@"
}

log_config() {
  log_with_prefix config "$@"
}

log_install() {
  log_with_prefix install "$@"
}

log_wait() {
  log_with_prefix wait "$@"
}

log_result() {
  log_with_prefix result "$@"
}

log_warn() {
  log_with_prefix warn "$*" >&2
}

log_error() {
  log_with_prefix error "$*" >&2
}

die() {
  log_error "$*"
  exit 1
}

prefix_stream() {
  local prefix="$1"
  while IFS= read -r line; do
    printf '[%s] %s\n' "${prefix}" "${line}"
  done
}

to_lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

require_command() {
  local command_name="$1"
  local description="${2:-$1}"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    die "${description} is required on PATH."
  fi
}

require_oc() {
  require_command oc "oc CLI"
}

render_template_file() {
  local template_path="$1"
  local out_path="$2"
  local vars_csv="${3:-}"

  local renderer_binary="${MAS_EST_RENDERER_BINARY:-${MAS_IAM_RENDERER_BINARY:-}}"
  if [[ -n "${renderer_binary}" && -x "${renderer_binary}" ]]; then
    "${renderer_binary}" render-template --vars "${vars_csv}" "${template_path}" "${out_path}"
    return
  fi

  require_command envsubst "envsubst"
  if [[ -n "${vars_csv}" ]]; then
    local -a render_vars=()
    local subst_vars=""
    local var_name=""
    IFS=',' read -r -a render_vars <<<"${vars_csv}"
    for var_name in "${render_vars[@]}"; do
      [[ -n "${var_name}" ]] || continue
      subst_vars="${subst_vars}\${${var_name}} "
    done
    envsubst "${subst_vars}" <"${template_path}" >"${out_path}"
  else
    envsubst <"${template_path}" >"${out_path}"
  fi
}

require_env_vars() {
  local variable_name=""

  for variable_name in "$@"; do
    if [[ -z "${!variable_name:-}" ]]; then
      die "Set ${variable_name}"
    fi
  done
}

ensure_oc_login() {
  if ! OC_WHOAMI="$(oc whoami 2>/dev/null)"; then
    die "cluster login check failed; run 'oc login' first."
  fi

  if ! OC_SERVER="$(oc whoami --show-server 2>/dev/null)"; then
    die "unable to determine the current cluster server with 'oc whoami --show-server'."
  fi
}

ensure_namespace_exists() {
  local namespace="$1"

  if ! oc get namespace "${namespace}" >/dev/null 2>&1; then
    log_install "creating namespace ${namespace}"
    oc create namespace "${namespace}" >/dev/null
  fi
}

render_namespace_manifest() {
  local input="$1"
  local output="$2"
  local namespace="$3"

  awk -v ns="${namespace}" '
    {
      gsub(/namespace: mas-est/, "namespace: " ns)
      gsub(/system:serviceaccount:mas-est:/, "system:serviceaccount:" ns ":")
      if ($0 ~ /^[[:space:]]*- mas-est$/) {
        sub(/mas-est$/, ns)
      }
      print
    }
  ' "${input}" > "${output}"
}

prime_last_applied_annotations() {
  local manifest_path="$1"
  local temp_dir=""
  local document_path=""

  if [[ -z "${manifest_path}" || ! -f "${manifest_path}" ]]; then
    return 0
  fi

  temp_dir="$(mktemp -d)"
  awk -v out_dir="${temp_dir}" '
    function next_file() {
      file = sprintf("%s/doc-%03d.yaml", out_dir, ++count)
    }

    BEGIN {
      next_file()
    }

    /^[[:space:]]*---[[:space:]]*$/ {
      next_file()
      next
    }

    {
      print >> file
    }
  ' "${manifest_path}"

  for document_path in "${temp_dir}"/doc-*.yaml; do
    [[ -f "${document_path}" ]] || continue
    if ! grep -q '[^[:space:]#]' "${document_path}"; then
      continue
    fi
    oc apply set-last-applied -f "${document_path}" --create-annotation=true >/dev/null 2>&1 || true
  done

  rm -rf "${temp_dir}"
}

# split_manifest_by_kind splits a multi-document manifest in two: documents
# whose top-level kind equals ${kind} are written to ${match_path}, everything
# else to ${rest_path}. Lets a caller apply a workload separately from the
# ConfigMaps/Secrets it consumes, so an ordering step can run in between.
# Pure awk on purpose - the published installer image has no python3.
split_manifest_by_kind() {
  local manifest_path="$1"
  local kind="$2"
  local match_path="$3"
  local rest_path="$4"

  if [[ -z "${manifest_path}" || ! -f "${manifest_path}" ]]; then
    return 1
  fi

  : > "${match_path}"
  : > "${rest_path}"

  awk -v want_kind="${kind}" -v match_path="${match_path}" -v rest_path="${rest_path}" '
    function flush_doc() {
      if (doc == "") {
        return
      }
      if (doc_kind == want_kind) {
        printf "---\n%s", doc >> match_path
      } else {
        printf "---\n%s", doc >> rest_path
      }
      doc = ""
      doc_kind = ""
    }

    /^[[:space:]]*---[[:space:]]*$/ {
      flush_doc()
      next
    }

    # Only a column-0 "kind:" is the document kind; anything indented belongs to
    # a nested object (a pod template, a subject list) and must not win.
    /^kind:[[:space:]]*[^[:space:]]/ {
      if (doc_kind == "") {
        doc_kind = $2
      }
    }

    {
      doc = doc $0 "\n"
    }

    END {
      flush_doc()
    }
  ' "${manifest_path}"
}

wait_for_namespaced_resource() {
  local kind="$1"
  local name="$2"
  local namespace="$3"
  local timeout_seconds="${4:-600}"
  local elapsed=0

  while (( elapsed < timeout_seconds )); do
    if oc get "${kind}/${name}" -n "${namespace}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done

  die "timed out waiting for ${kind}/${name} in namespace ${namespace}"
}

# When this run started, as ISO-8601 UTC so it compares lexically with
# Kubernetes creationTimestamps (portable: no GNU/BSD `date -d` differences).
MAS_EST_RUN_STARTED_AT="${MAS_EST_RUN_STARTED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

# job_true_conditions prints the type of every True condition on a Job, one
# per line (Complete, Failed, FailureTarget, SuccessCriteriaMet, ...).
job_true_conditions() {
  local namespace="$1" job="$2"
  oc get job "${job}" -n "${namespace}" \
    -o jsonpath='{range .status.conditions[?(@.status=="True")]}{.type}{"\n"}{end}' 2>/dev/null || true
}

# print_job_diagnostics shows why a Job failed: its Failed condition, its
# pods' phases and waiting reasons, and the tail of every pod's logs.
print_job_diagnostics() {
  local namespace="$1" job="$2"
  local reason pod
  reason="$(oc get job "${job}" -n "${namespace}" \
    -o jsonpath='{range .status.conditions[?(@.status=="True")]}{.type}{": "}{.reason}{" - "}{.message}{"\n"}{end}' 2>/dev/null || true)"
  if [[ -n "${reason}" ]]; then
    printf '%s\n' "${reason}" | prefix_stream "job/${job}" >&2
  fi
  oc get pods -n "${namespace}" -l "job-name=${job}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{" phase="}{.status.phase}{" "}{range .status.containerStatuses[*]}{.state.waiting.reason}{" "}{.state.waiting.message}{" "}{.state.terminated.reason}{" exit="}{.state.terminated.exitCode}{end}{"\n"}{end}' \
    2>/dev/null | prefix_stream "job/${job} pod" >&2 || true
  while IFS= read -r pod; do
    [[ -n "${pod}" ]] || continue
    log_error "last log lines of ${pod}:"
    oc logs -n "${namespace}" "${pod}" --all-containers --tail=80 2>&1 | prefix_stream "${pod##*/}" >&2 || true
  done < <(oc get pods -n "${namespace}" -l "job-name=${job}" -o name 2>/dev/null || true)
}

# job_image_pull_stuck prints the pod waiting reason when a Job's pod cannot
# pull or start its image, and nothing otherwise. Kubernetes never fails a Job
# for that, so without this a bad image burns the whole wait timeout.
job_image_pull_stuck() {
  local namespace="$1" job="$2"
  oc get pods -n "${namespace}" -l "job-name=${job}" \
    -o jsonpath='{range .items[*].status.containerStatuses[*]}{.state.waiting.reason}{"\n"}{end}' 2>/dev/null \
    | grep -E -m1 '^(ImagePullBackOff|ErrImagePull|InvalidImageName|CreateContainerConfigError)$' || true
}

# wait_for_job_complete waits for a Job to reach Complete OR Failed, instead
# of `oc wait --for=condition=complete`, which sits out its whole timeout
# (10-20 minutes) on a Job that has already failed. On failure it prints the
# Job's condition, pod states and pod logs, then exits non-zero.
#
# A Job that is ALREADY Failed and was created before this run started is
# usually left over from an earlier run (Helm hooks such as
# mas-est-iam-ldap-config are not re-run on their own). It gets a grace period
# to be replaced before the wait gives up with the stale-job hint ($4).
wait_for_job_complete() {
  local namespace="$1"
  local job="$2"
  local timeout_seconds="${3:-1200}"
  local stale_hint="${4:-delete it with 'oc delete job ${job} -n ${namespace}' and re-run the install}"
  local stale_grace="${MAS_EST_STALE_JOB_GRACE_SECONDS:-180}"
  local image_pull_grace="${MAS_EST_IMAGE_PULL_GRACE_SECONDS:-180}"
  local waited=0 stuck_for=0 conditions created stuck
  local interval="${MAS_EST_JOB_POLL_INTERVAL:-5}"

  while (( waited < timeout_seconds )); do
    conditions="$(job_true_conditions "${namespace}" "${job}")"
    if grep -qxE 'Complete|SuccessCriteriaMet' <<<"${conditions}"; then
      log_wait "job/${job} complete (after ${waited}s)"
      return 0
    fi
    if grep -qxE 'Failed|FailureTarget' <<<"${conditions}"; then
      created="$(oc get job "${job}" -n "${namespace}" -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null || true)"
      if [[ -n "${created}" && "${created}" < "${MAS_EST_RUN_STARTED_AT}" ]]; then
        if (( waited < stale_grace )); then
          if (( waited % 60 == 0 )); then
            log_wait "job/${job} is Failed from an earlier run (created ${created}); waiting up to ${stale_grace}s for it to be replaced"
          fi
          sleep "${interval}"
          waited=$((waited + interval))
          continue
        fi
        print_job_diagnostics "${namespace}" "${job}"
        die "job/${job} in namespace ${namespace} is still the Failed Job from an earlier run (created ${created}). Fix the cause shown above, then ${stale_hint}."
      fi
      print_job_diagnostics "${namespace}" "${job}"
      die "job/${job} in namespace ${namespace} failed; see the condition, pod states and logs above."
    fi

    stuck="$(job_image_pull_stuck "${namespace}" "${job}")"
    if [[ -n "${stuck}" ]]; then
      stuck_for=$((stuck_for + interval))
      if (( stuck_for >= image_pull_grace )); then
        print_job_diagnostics "${namespace}" "${job}"
        die "job/${job} in namespace ${namespace} cannot start its pod (${stuck} for ${stuck_for}s); see the pod state above."
      fi
    else
      stuck_for=0
    fi

    if (( waited > 0 && waited % 60 == 0 )); then
      log_wait "still waiting for job/${job} (${waited}s of ${timeout_seconds}s)"
    fi
    sleep "${interval}"
    waited=$((waited + interval))
  done

  print_job_diagnostics "${namespace}" "${job}"
  die "timed out after ${timeout_seconds}s waiting for job/${job} in namespace ${namespace} to complete"
}

# delete_job_if_failed removes a Job left Failed by an earlier run so the
# apply that follows can create it afresh (a Job's pod template is immutable,
# so re-applying the same manifest would leave the failed one in place).
delete_job_if_failed() {
  local namespace="$1" job="$2"
  if grep -qxE 'Failed|FailureTarget' <<<"$(job_true_conditions "${namespace}" "${job}")"; then
    log_install "deleting job/${job}, left Failed by an earlier run, so it is recreated"
    oc delete job "${job}" -n "${namespace}" --ignore-not-found >/dev/null
  fi
}

# wait_for_ldaps_listening waits until slapd accepts connections on 636 inside
# the OpenLDAP pod. The deployment reports Ready as soon as the container
# starts, but on first start the image generates 2048-bit DH parameters before
# slapd listens, which takes from seconds to several minutes. Keycloak federates
# users to LDAP, so anything that writes users before then fails with
# "Connection refused".
wait_for_ldaps_listening() {
  local namespace="$1"
  local deployment="$2"
  local timeout_seconds="${3:-1200}"
  local waited=0
  while (( waited < timeout_seconds )); do
    if oc exec -n "${namespace}" "deployment/${deployment}" -c openldap -- \
        bash -c 'exec 3<>/dev/tcp/127.0.0.1/636' >/dev/null 2>&1; then
      log_wait "openldap is accepting LDAPS connections (after ${waited}s)"
      return 0
    fi
    if (( waited % 60 == 0 )); then
      log_wait "waiting for openldap to accept LDAPS connections (first start generates TLS parameters; can take several minutes)"
    fi
    sleep 10
    waited=$(( waited + 10 ))
  done
  die "openldap in namespace ${namespace} did not accept LDAPS connections within ${timeout_seconds}s; check: oc logs deployment/${deployment} -n ${namespace} -c openldap"
}

find_operator_csv_name() {
  local namespace="$1"
  local csv=""
  local candidate=""

  csv="$(oc get subscription.operators.coreos.com/mas-iam-operator -n "${namespace}" -o jsonpath='{.status.currentCSV}' 2>/dev/null || true)"
  if [[ -n "${csv}" ]]; then
    printf '%s' "${csv}"
    return 0
  fi

  csv="$(oc get csv -n "${namespace}" -l "operators.coreos.com/mas-iam-operator.${namespace}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "${csv}" ]]; then
    printf '%s' "${csv}"
    return 0
  fi

  while IFS= read -r candidate; do
    case "${candidate}" in
      mas-iam-operator.v*)
        printf '%s' "${candidate}"
        return 0
        ;;
    esac
  done < <(oc get csv -n "${namespace}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)

  return 1
}

# expected_operator_csv prints the CSV the catalog currently offers as the
# head of the Subscription's channel (packagemanifest currentCSV).
expected_operator_csv() {
  local namespace="$1"
  local channel
  channel="$(oc get subscription.operators.coreos.com/mas-iam-operator -n "${namespace}" -o jsonpath='{.spec.channel}' 2>/dev/null || true)"
  channel="${channel:-alpha}"
  oc get packagemanifests mas-iam-operator -n openshift-marketplace \
    -o jsonpath="{.status.channels[?(@.name==\"${channel}\")].currentCSV}" 2>/dev/null || true
}

# csv_version_older succeeds when CSV $1 (e.g. mas-iam-operator.v0.0.14) has
# a lower version than CSV $2. Numeric dotted compare; anything it cannot
# parse counts as "not older", so the caller never acts on a guess.
csv_version_older() {
  local a="${1##*.v}" b="${2##*.v}"
  a="${a%%-*}"
  b="${b%%-*}"
  local -a av bv
  IFS=. read -r -a av <<<"${a}"
  IFS=. read -r -a bv <<<"${b}"
  local i x y n=${#av[@]}
  (( ${#bv[@]} > n )) && n=${#bv[@]}
  for (( i = 0; i < n; i++ )); do
    x="${av[i]:-0}"
    y="${bv[i]:-0}"
    if ! [[ "${x}" =~ ^[0-9]+$ && "${y}" =~ ^[0-9]+$ ]]; then
      return 1
    fi
    if (( 10#${x} < 10#${y} )); then
      return 0
    fi
    if (( 10#${x} > 10#${y} )); then
      return 1
    fi
  done
  return 1
}

# remove_stale_operator deletes the Subscription and the operator CSVs so the
# Subscription can be re-applied and resolve afresh to the catalog head; the
# same clean-up scripts/install-operator-from-catalog.sh does. CRDs and the
# MasIamStack CR are left alone, so the stack itself keeps running.
remove_stale_operator() {
  local namespace="$1"
  local csv
  oc delete subscription.operators.coreos.com mas-iam-operator -n "${namespace}" --ignore-not-found >/dev/null
  while IFS= read -r csv; do
    [[ -n "${csv}" ]] || continue
    oc delete csv "${csv}" -n "${namespace}" --ignore-not-found >/dev/null
  done < <(oc get csv -n "${namespace}" -l "operators.coreos.com/mas-iam-operator.${namespace}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
}

# wait_for_operator_csv_succeeded waits for the operator CSV to reach
# Succeeded AND be the catalog's current head. Accepting any Succeeded CSV let
# a stale operator from an earlier install (OLM resolved an older CSV, or has
# no upgrade path to the new one) run the new stack. An older CSV gets
# MAS_EST_STALE_CSV_GRACE_SECONDS (default 120) to be upgraded by OLM; after
# that, when $3 names the Subscription manifest, the stale Subscription and
# CSV are removed and the Subscription re-applied once. A CSV stuck in Failed
# for MAS_EST_CSV_FAILED_GRACE_SECONDS (default 300) fails with its message.
wait_for_operator_csv_succeeded() {
  local namespace="$1"
  local timeout_seconds="${2:-900}"
  local subscription_manifest="${3:-}"
  local stale_grace="${MAS_EST_STALE_CSV_GRACE_SECONDS:-120}"
  local failed_grace="${MAS_EST_CSV_FAILED_GRACE_SECONDS:-300}"
  local interval="${MAS_EST_CSV_POLL_INTERVAL:-10}"
  local elapsed=0 stale_for=0 failed_for=0 replaced=0
  local csv="" phase="" expected="" message="" last_message=""

  while (( elapsed < timeout_seconds )); do
    csv="$(find_operator_csv_name "${namespace}" || true)"
    phase=""
    if [[ -n "${csv}" ]]; then
      phase="$(oc get csv -n "${namespace}" "${csv}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    fi

    log_wait "operator_csv=${csv:-missing} phase=${phase:-missing}"
    if [[ "${phase}" == "Succeeded" ]]; then
      expected="$(expected_operator_csv "${namespace}")"
      # Only an installed CSV provably OLDER than the head counts as stale:
      # packageserver can briefly serve older catalog content, and a newer
      # installed operator must never be torn down for it.
      if [[ -z "${expected}" ]] || ! csv_version_older "${csv}" "${expected}"; then
        WAITED_OPERATOR_CSV="${csv}"
        return 0
      fi
      if (( stale_for == 0 )); then
        log_warn "installed operator ${csv} is OLDER than the catalog's current ${expected}; waiting up to ${stale_grace}s for OLM to upgrade it"
      fi
      stale_for=$((stale_for + interval))
      if (( stale_for >= stale_grace )); then
        if [[ -n "${subscription_manifest}" && -s "${subscription_manifest}" && "${replaced}" == "0" ]]; then
          log_warn "operator ${csv} is stale (catalog offers ${expected}); removing the Subscription and CSV and re-subscribing"
          remove_stale_operator "${namespace}"
          oc apply -f "${subscription_manifest}"
          replaced=1
          stale_for=0
        else
          die "the MAS EST IAM operator is stuck on ${csv} while the catalog offers ${expected}. Remove the stale operator ('oc delete subscription mas-iam-operator -n ${namespace}' and 'oc delete csv ${csv} -n ${namespace}') and re-run the install."
        fi
      fi
    else
      stale_for=0
    fi

    if [[ "${phase}" == "Failed" ]]; then
      message="$(oc get csv -n "${namespace}" "${csv}" -o jsonpath='{.status.reason}{": "}{.status.message}' 2>/dev/null || true)"
      if [[ "${message}" != "${last_message}" ]]; then
        log_warn "operator CSV ${csv} is Failed: ${message}"
        last_message="${message}"
      fi
      failed_for=$((failed_for + interval))
      if (( failed_for >= failed_grace )); then
        die "the MAS EST IAM operator CSV ${csv} has been Failed for ${failed_for}s: ${message}"
      fi
    else
      failed_for=0
    fi

    sleep "${interval}"
    elapsed=$((elapsed + interval))
  done

  if [[ -n "${csv}" ]]; then
    message="$(oc get csv -n "${namespace}" "${csv}" -o jsonpath='{.status.reason}{": "}{.status.message}' 2>/dev/null || true)"
    [[ -n "${message}" && "${message}" != ": " ]] && log_error "operator CSV ${csv} last status: ${message}"
  fi
  oc get subscription.operators.coreos.com/mas-iam-operator -n "${namespace}" \
    -o jsonpath='{range .status.conditions[*]}{.type}{"="}{.status}{" "}{.reason}{" "}{.message}{"\n"}{end}' 2>/dev/null \
    | prefix_stream "subscription" >&2 || true
  die "timed out waiting for the MAS EST IAM operator CSV to reach Succeeded in namespace ${namespace}"
}

# wait_for_catalog_source_ready waits for a CatalogSource in
# openshift-marketplace to report READY and fails fast, with the catalog
# pod's waiting reason, when its image cannot be pulled or the connection
# stays in TRANSIENT_FAILURE (MAS_EST_CATALOG_FAILURE_GRACE_SECONDS, default
# 90; OLM reports TRANSIENT_FAILURE briefly while the pod starts). It used to
# log "proceeding anyway" and the install then timed out ~25 minutes later
# with a generic CSV message.
wait_for_catalog_source_ready() {
  local name="$1"
  local timeout_seconds="${2:-300}"
  local failure_grace="${MAS_EST_CATALOG_FAILURE_GRACE_SECONDS:-90}"
  local interval="${MAS_EST_CATALOG_POLL_INTERVAL:-5}"
  local waited=0 transient_for=0 state pods image
  while (( waited < timeout_seconds )); do
    state="$(oc -n openshift-marketplace get catalogsource "${name}" -o jsonpath='{.status.connectionState.lastObservedState}' 2>/dev/null || true)"
    if [[ "${state}" == "READY" ]]; then
      log_wait "catalog_source=${name} state=READY"
      return 0
    fi
    pods="$(oc -n openshift-marketplace get pods -l "olm.catalogSource=${name}" \
      -o jsonpath='{range .items[*]}{.metadata.name}{" "}{range .status.containerStatuses[*]}{.state.waiting.reason}{" "}{.state.waiting.message}{end}{"\n"}{end}' 2>/dev/null || true)"
    if grep -qE 'ImagePullBackOff|ErrImagePull|InvalidImageName' <<<"${pods}"; then
      image="$(oc -n openshift-marketplace get catalogsource "${name}" -o jsonpath='{.spec.image}' 2>/dev/null || true)"
      printf '%s\n' "${pods}" | prefix_stream "catalog-pod" >&2
      die "CatalogSource ${name} cannot pull its image ${image:-<unknown>}; see the catalog pod state above"
    fi
    if [[ "${state}" == "TRANSIENT_FAILURE" ]]; then
      transient_for=$((transient_for + interval))
      if (( transient_for >= failure_grace )); then
        [[ -n "${pods}" ]] && printf '%s\n' "${pods}" | prefix_stream "catalog-pod" >&2
        die "CatalogSource ${name} has been in TRANSIENT_FAILURE for ${transient_for}s; check: oc -n openshift-marketplace get pods -l olm.catalogSource=${name}"
      fi
    else
      transient_for=0
    fi
    sleep "${interval}"
    waited=$((waited + interval))
  done
  [[ -n "${pods:-}" ]] && printf '%s\n' "${pods}" | prefix_stream "catalog-pod" >&2
  die "CatalogSource ${name} did not report READY within ${timeout_seconds}s (last state: ${state:-none})"
}

list_storage_classes() {
  oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
}

# Provisioner of statically provisioned classes such as ODF's `localblock`:
# their PersistentVolumes are pre-created, so a new PVC never binds. Mirrors
# oc.StaticProvisioner / StorageClass.IsStatic in the Go installer.
STATIC_STORAGE_PROVISIONER="kubernetes.io/no-provisioner"

# list_static_storage_classes prints the classes whose provisioner is static.
list_static_storage_classes() {
  local name provisioner
  while IFS=$'\t' read -r name provisioner; do
    if [[ -n "${name}" && "${provisioner}" == "${STATIC_STORAGE_PROVISIONER}" ]]; then
      printf '%s\n' "${name}"
    fi
  done < <(oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.provisioner}{"\n"}{end}' 2>/dev/null || true)
}

is_static_storage_class() {
  local candidate="$1"
  local existing
  for existing in "${STATIC_STORAGE_CLASSES[@]:-}"; do
    if [[ -n "${existing}" && "${existing}" == "${candidate}" ]]; then
      return 0
    fi
  done
  return 1
}

get_default_storage_class() {
  local name stable_default beta_default

  while IFS=$'\t' read -r name stable_default beta_default; do
    if [[ -z "${name}" ]]; then
      continue
    fi
    if [[ "${stable_default}" == "true" || "${beta_default}" == "true" ]]; then
      printf '%s' "${name}"
      return 0
    fi
  done < <(
    oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\t"}{.metadata.annotations.storageclass\.beta\.kubernetes\.io/is-default-class}{"\n"}{end}' 2>/dev/null || true
  )

  return 1
}

storage_class_exists() {
  local candidate="$1"
  local existing

  for existing in "${ALL_STORAGE_CLASSES[@]:-}"; do
    if [[ "${existing}" == "${candidate}" ]]; then
      return 0
    fi
  done

  return 1
}

is_cephfs_storage_class() {
  local candidate
  candidate="$(to_lower "$1")"
  [[ "${candidate}" == *cephfs* ]]
}

discover_storage_classes() {
  local existing lower_name
  local -a preferred_names=(
    "ocs-external-storagecluster-ceph-rbd"
    "ocs-storagecluster-ceph-rbd"
    "odf-storagecluster-ceph-rbd"
    "rook-ceph-block"
  )

  ALL_STORAGE_CLASSES=()
  STATIC_STORAGE_CLASSES=()
  DEFAULT_STORAGE_CLASS=""
  PREFERRED_BLOCK_STORAGE_CLASS=""
  REGEX_BLOCK_STORAGE_CLASS=""

  while IFS= read -r existing; do
    if [[ -n "${existing}" ]]; then
      ALL_STORAGE_CLASSES+=("${existing}")
    fi
  done < <(list_storage_classes)

  if (( ${#ALL_STORAGE_CLASSES[@]} == 0 )); then
    return 1
  fi

  while IFS= read -r existing; do
    if [[ -n "${existing}" ]]; then
      STATIC_STORAGE_CLASSES+=("${existing}")
    fi
  done < <(list_static_storage_classes)

  DEFAULT_STORAGE_CLASS="$(get_default_storage_class || true)"

  # Static (no-provisioner) classes are never auto-selected: `localblock`
  # matches the block regex below, yet a PVC against it stays Pending forever.
  for existing in "${preferred_names[@]}"; do
    if storage_class_exists "${existing}" && ! is_static_storage_class "${existing}"; then
      PREFERRED_BLOCK_STORAGE_CLASS="${existing}"
      break
    fi
  done

  for existing in "${ALL_STORAGE_CLASSES[@]}"; do
    if is_static_storage_class "${existing}"; then
      continue
    fi
    lower_name="$(to_lower "${existing}")"
    if [[ "${lower_name}" =~ (rbd|block) ]]; then
      REGEX_BLOCK_STORAGE_CLASS="${existing}"
      break
    fi
  done
}

preferred_block_storage_class() {
  if [[ -n "${PREFERRED_BLOCK_STORAGE_CLASS:-}" ]]; then
    printf '%s' "${PREFERRED_BLOCK_STORAGE_CLASS}"
    return 0
  fi

  if [[ -n "${REGEX_BLOCK_STORAGE_CLASS:-}" ]]; then
    printf '%s' "${REGEX_BLOCK_STORAGE_CLASS}"
    return 0
  fi

  return 1
}

select_postgres_storage_class() {
  local explicit_override="${1:-${POSTGRES_STORAGE_CLASS:-}}"

  if ! discover_storage_classes; then
    return 1
  fi

  SELECTED_STORAGE_CLASS=""
  SELECTED_STORAGE_REASON=""

  if [[ -n "${explicit_override}" ]]; then
    SELECTED_STORAGE_CLASS="${explicit_override}"
    SELECTED_STORAGE_REASON="explicit-override"
    return 0
  fi

  if [[ -n "${PREFERRED_BLOCK_STORAGE_CLASS}" ]]; then
    SELECTED_STORAGE_CLASS="${PREFERRED_BLOCK_STORAGE_CLASS}"
    SELECTED_STORAGE_REASON="preferred-block-name"
    return 0
  fi

  if [[ -n "${REGEX_BLOCK_STORAGE_CLASS}" ]]; then
    SELECTED_STORAGE_CLASS="${REGEX_BLOCK_STORAGE_CLASS}"
    SELECTED_STORAGE_REASON="regex-block-match"
    return 0
  fi

  if [[ -n "${DEFAULT_STORAGE_CLASS}" ]] && ! is_static_storage_class "${DEFAULT_STORAGE_CLASS}"; then
    SELECTED_STORAGE_CLASS="${DEFAULT_STORAGE_CLASS}"
    SELECTED_STORAGE_REASON="cluster-default"
    return 0
  fi

  local candidate
  for candidate in "${ALL_STORAGE_CLASSES[@]}"; do
    if ! is_static_storage_class "${candidate}"; then
      SELECTED_STORAGE_CLASS="${candidate}"
      SELECTED_STORAGE_REASON="first-available"
      return 0
    fi
  done

  # Only static classes exist: nothing will bind; say so instead of guessing.
  SELECTED_STORAGE_CLASS="${ALL_STORAGE_CLASSES[0]}"
  SELECTED_STORAGE_REASON="first-available-static-will-not-bind"
  log_warn "every StorageClass uses ${STATIC_STORAGE_PROVISIONER}; new PVCs will not bind. Set POSTGRES_STORAGE_CLASS / --storage-class to a dynamically provisioned class"
}

warn_if_cephfs_default_has_block_candidate() {
  local default_sc="${1:-${DEFAULT_STORAGE_CLASS:-}}"
  local block_candidate="${2:-}"

  if [[ -z "${block_candidate}" ]]; then
    block_candidate="$(preferred_block_storage_class || true)"
  fi

  if [[ -n "${default_sc}" && -n "${block_candidate}" ]] \
    && is_cephfs_storage_class "${default_sc}" \
    && [[ "${default_sc}" != "${block_candidate}" ]]; then
    log_warn "cluster default StorageClass is ${default_sc}; preferred block/RBD class ${block_candidate} is also available"
  fi
}

parse_host_from_url() {
  local url="$1"
  local host=""

  case "${url}" in
    http://http://*|https://https://*|http://https://*|https://http://*)
      return 2
      ;;
    http://*|https://*)
      ;;
    *)
      return 1
      ;;
  esac

  host="${url#*://}"
  host="${host%%/*}"
  host="${host%%:*}"

  if [[ -z "${host}" ]]; then
    return 1
  fi

  printf '%s' "${host}"
}

lookup_routes_by_host() {
  local host="$1"

  oc get route -A \
    -o jsonpath="{range .items[?(@.spec.host=='${host}')]}{.metadata.namespace}{'/'}{.metadata.name}{'\n'}{end}" \
    2>/dev/null || true
}

get_route_ca_certificate() {
  local route_ref="$1"
  local route_namespace="${route_ref%%/*}"
  local route_name="${route_ref##*/}"

  oc get route -n "${route_namespace}" "${route_name}" -o jsonpath='{.spec.tls.caCertificate}' 2>/dev/null || true
}
