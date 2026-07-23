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

wait_for_operator_csv_succeeded() {
  local namespace="$1"
  local timeout_seconds="${2:-900}"
  local elapsed=0
  local csv=""
  local phase=""

  while (( elapsed < timeout_seconds )); do
    csv="$(find_operator_csv_name "${namespace}" || true)"
    phase=""
    if [[ -n "${csv}" ]]; then
      phase="$(oc get csv -n "${namespace}" "${csv}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    fi

    log_wait "operator_csv=${csv:-missing} phase=${phase:-missing}"
    if [[ "${phase}" == "Succeeded" ]]; then
      WAITED_OPERATOR_CSV="${csv}"
      return 0
    fi

    sleep 10
    elapsed=$((elapsed + 10))
  done

  die "timed out waiting for the MAS EST IAM operator CSV to reach Succeeded in namespace ${namespace}"
}

list_storage_classes() {
  oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
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

  DEFAULT_STORAGE_CLASS="$(get_default_storage_class || true)"

  for existing in "${preferred_names[@]}"; do
    if storage_class_exists "${existing}"; then
      PREFERRED_BLOCK_STORAGE_CLASS="${existing}"
      break
    fi
  done

  for existing in "${ALL_STORAGE_CLASSES[@]}"; do
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

  if [[ -n "${DEFAULT_STORAGE_CLASS}" ]]; then
    SELECTED_STORAGE_CLASS="${DEFAULT_STORAGE_CLASS}"
    SELECTED_STORAGE_REASON="cluster-default"
    return 0
  fi

  SELECTED_STORAGE_CLASS="${ALL_STORAGE_CLASSES[0]}"
  SELECTED_STORAGE_REASON="first-available"
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
