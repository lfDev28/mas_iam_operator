#!/usr/bin/env bash
# Check the image pin list (images.yaml) against the registries and the tree.
#
#   scripts/check-images.sh               # both checks below
#   scripts/check-images.sh --scan-only   # (b) only: offline, no network
#   scripts/check-images.sh --registry-only
#   scripts/check-images.sh --expect-unpublished REF   # REF may be missing (repeatable)
#   scripts/check-images.sh --ref REF --archs amd64,arm64 [--label KEY=VALUE]
#                                                      # one ref only (release verify)
#
# (a) Registry: every `images:` ref in images.yaml must exist on its registry
#     (anonymous registry v2 manifest GET, with the bearer-token dance that
#     quay.io, docker.io, ghcr.io, gcr.io and registry.access.redhat.com use),
#     and must carry every arch listed in its `archs:`. Entries marked
#     `optional: true` only warn. Needs curl and jq; no podman/skopeo.
# (b) Tree: every image ref found in the tracked tree (Go string literals,
#     Helm values, the CSV, manifests, scripts, Dockerfiles/Containerfiles, and
#     registry/repository/tag triples in YAML/JSON) must be listed in
#     images.yaml under `images:` or `allowlist:`.
#
# Exit status: 0 when nothing failed (warnings allowed), 1 otherwise.
#
# Why this exists: past releases broke because an upstream image vanished
# (MinIO), a floating tag was withdrawn (ose-cli :latest) and a catalog tag was
# referenced before it was pushed. All three are a 404 this script would catch.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGES_FILE="${IMAGES_FILE:-${ROOT_DIR}/images.yaml}"

do_registry=true
do_scan=true
verbose=false
expect_unpublished=" "
single_ref=""
single_archs=""
single_label=""

usage() {
  sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --scan-only) do_registry=false ;;
    --registry-only) do_scan=false ;;
    -v|--verbose) verbose=true ;;
    --expect-unpublished) expect_unpublished+="${2:-} "; shift ;;
    --ref) single_ref="${2:-}"; do_scan=false; shift ;;
    --archs) single_archs="${2:-}"; shift ;;
    --label) single_label="${2:-}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

[[ -f "${IMAGES_FILE}" ]] || { echo "error: ${IMAGES_FILE} not found" >&2; exit 2; }

failures=0
warnings=0
fail() { echo "FAIL  $*"; failures=$((failures + 1)); }
warn() { echo "WARN  $*"; warnings=$((warnings + 1)); }
ok()   { echo "ok    $*"; }

# ---------------------------------------------------------------------------
# images.yaml parser. The file uses a deliberately flat schema so it can be
# read without yq: two sections (images:, allowlist:), each a list of maps
# whose first key is `- ref:` and whose values are scalars or [a, b] lists.
# Emits one TSV row per entry: section ref kind archs optional used_in
# ---------------------------------------------------------------------------
parse_images_file() {
  awk '
    function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
    function unquote(s) { s = trim(s); if (s ~ /^".*"$/ || s ~ /^\x27.*\x27$/) s = substr(s, 2, length(s) - 2); return s }
    function flush() {
      if (ref != "") printf "%s|%s|%s|%s|%s|%s\n", section, ref, kind, archs, optional, used
      ref = ""; kind = ""; archs = ""; optional = "false"; used = ""
    }
    /^[ \t]*#/ || /^[ \t]*$/ { next }
    /^images:/    { flush(); section = "images"; next }
    /^allowlist:/ { flush(); section = "allowlist"; next }
    /^[^ \t-]/    { flush(); section = ""; next }
    {
      line = $0
      if (line ~ /^[ \t]*- ref:/) { flush(); sub(/^[ \t]*- ref:/, "", line); ref = unquote(line); next }
      if (match(line, /^[ \t]+[a-z_]+:/)) {
        key = trim(substr(line, 1, RLENGTH - 1)); val = substr(line, RLENGTH + 1)
        sub(/[ \t]+#.*$/, "", val); val = trim(val)
        if (val ~ /^\[/) { gsub(/[\[\] \t"]/, "", val) } else { val = unquote(val) }
        if (key == "kind") kind = val
        else if (key == "archs") archs = val
        else if (key == "optional") optional = val
        else if (key == "used_in") used = val
      }
    }
    END { flush() }
  ' "${IMAGES_FILE}"
}

# Normalise a ref so docker.io shorthand compares equal to the long form:
#   golang:1.25 -> docker.io/library/golang:1.25
#   docker.io/foo:1 -> docker.io/library/foo:1
normalize_ref() {
  local ref="$1" first rest
  first="${ref%%/*}"
  if [[ "${ref}" != */* ]] || [[ "${first}" != *.* && "${first}" != *:* && "${first}" != localhost ]]; then
    ref="docker.io/${ref}"
  fi
  rest="${ref#docker.io/}"
  if [[ "${ref}" == docker.io/* && "${rest}" != */* ]]; then
    ref="docker.io/library/${rest}"
  fi
  printf '%s\n' "${ref}"
}

# ---------------------------------------------------------------------------
# (a) Registry existence + arch check
# ---------------------------------------------------------------------------
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

ACCEPT_HEADER="application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

# Bearer tokens are cached per host+repo as files (bash 3.2 has no assoc arrays).
token_file() { printf '%s/token-%s' "${TMP_DIR}" "$(printf '%s' "$1" | tr '/:' '__')"; }

# Sets: REG_HOST API_HOST REPO REFERENCE
split_ref() {
  local ref; ref="$(normalize_ref "$1")"
  REG_HOST="${ref%%/*}"
  local path="${ref#*/}"
  if [[ "${path}" == *@* ]]; then
    REPO="${path%@*}"; REFERENCE="${path#*@}"
  else
    REPO="${path%:*}"; REFERENCE="${path##*:}"
    [[ "${path}" == *:* ]] || REFERENCE="latest"
  fi
  case "${REG_HOST}" in
    docker.io) API_HOST="registry-1.docker.io" ;;
    *) API_HOST="${REG_HOST}" ;;
  esac
}

# registry_get <url> <out-file> [accept]; echoes the HTTP status. Performs the
# anonymous token dance on 401 and caches the token per host+repo.
registry_get() {
  local url="$1" out="$2" accept="${3:-${ACCEPT_HEADER}}"
  local tf code challenge realm service token=""
  tf="$(token_file "${API_HOST}/${REPO}")"
  [[ -s "${tf}" ]] && token="$(cat "${tf}")"
  code="$(curl_get "${url}" "${out}" "${accept}" "${token}")"
  if [[ "${code}" == 401 && -z "${token}" ]]; then
    challenge="$(grep -i '^www-authenticate:' "${TMP_DIR}/headers" | tail -1 | tr -d '\r')"
    realm="$(sed -n 's/.*realm="\([^"]*\)".*/\1/p' <<<"${challenge}")"
    service="$(sed -n 's/.*service="\([^"]*\)".*/\1/p' <<<"${challenge}")"
    if [[ -n "${realm}" ]]; then
      token="$(curl -sS --max-time 30 -G "${realm}" \
          --data-urlencode "service=${service}" --data-urlencode "scope=repository:${REPO}:pull" 2>/dev/null \
        | jq -r '.token // .access_token // empty' 2>/dev/null || true)"
      if [[ -n "${token}" ]]; then
        printf '%s' "${token}" >"${tf}"
        code="$(curl_get "${url}" "${out}" "${accept}" "${token}")"
      fi
    fi
  fi
  echo "${code}"
}

# curl_get <url> <out> <accept> [token]; echoes the HTTP status (000 on error).
# -L follows blob redirects to CDNs; curl drops the Authorization header when
# the redirect changes host, which is what presigned CDN URLs require.
curl_get() {
  local code=""
  if [[ -n "${4:-}" ]]; then
    code="$(curl -sSL --max-time 30 -o "$2" -D "${TMP_DIR}/headers" -w '%{http_code}' \
      -H "Accept: $3" -H "Authorization: Bearer $4" "$1" 2>/dev/null)" || true
  else
    code="$(curl -sSL --max-time 30 -o "$2" -D "${TMP_DIR}/headers" -w '%{http_code}' \
      -H "Accept: $3" "$1" 2>/dev/null)" || true
  fi
  echo "${code:-000}"
}

# image_archs <ref>: prints a comma-separated arch list, or MISSING:<code>.
image_archs() {
  split_ref "$1"
  local body="${TMP_DIR}/manifest.json" code media digest
  code="$(registry_get "https://${API_HOST}/v2/${REPO}/manifests/${REFERENCE}" "${body}")"
  if [[ "${code}" != 200 ]]; then
    echo "MISSING:${code}"
    return
  fi
  media="$(jq -r '.mediaType // empty' "${body}" 2>/dev/null || true)"
  if jq -e '.manifests' "${body}" >/dev/null 2>&1; then
    jq -r '[.manifests[] | .platform.architecture // empty | select(. != "unknown")] | unique | join(",")' "${body}"
    return
  fi
  if [[ "${media}" == *distribution.manifest.v1* ]] || jq -e '.schemaVersion == 1' "${body}" >/dev/null 2>&1; then
    jq -r '.architecture // "unknown"' "${body}"
    return
  fi
  digest="$(jq -r '.config.digest // empty' "${body}")"
  if [[ -z "${digest}" ]]; then
    echo "unknown"
    return
  fi
  code="$(registry_get "https://${API_HOST}/v2/${REPO}/blobs/${digest}" "${TMP_DIR}/config.json" "*/*")"
  if [[ "${code}" != 200 ]]; then
    echo "unknown"
    return
  fi
  jq -r '.architecture // "unknown"' "${TMP_DIR}/config.json"
}

# image_label <ref> <key>: prints the image config label (of the first
# platform for a multi-arch index), or nothing.
image_label() {
  split_ref "$1"
  local body="${TMP_DIR}/label-manifest.json" digest
  [[ "$(registry_get "https://${API_HOST}/v2/${REPO}/manifests/${REFERENCE}" "${body}")" == 200 ]] || return 0
  if jq -e '.manifests' "${body}" >/dev/null 2>&1; then
    digest="$(jq -r '[.manifests[] | select((.platform.architecture // "") != "unknown")][0].digest // empty' "${body}")"
    [[ -n "${digest}" ]] || return 0
    [[ "$(registry_get "https://${API_HOST}/v2/${REPO}/manifests/${digest}" "${body}")" == 200 ]] || return 0
  fi
  digest="$(jq -r '.config.digest // empty' "${body}")"
  [[ -n "${digest}" ]] || return 0
  [[ "$(registry_get "https://${API_HOST}/v2/${REPO}/blobs/${digest}" "${TMP_DIR}/label-config.json" "*/*")" == 200 ]] || return 0
  jq -r --arg k "$2" '.config.Labels[$k] // empty' "${TMP_DIR}/label-config.json"
}

check_registry() {
  if [[ -n "${single_ref}" ]]; then
    echo "== registry: ${single_ref} [${single_archs:-any arch}]"
  else
    echo "== (a) registry: every images.yaml ref exists with its archs"
  fi
  command -v curl >/dev/null || { fail "curl is required"; return; }
  command -v jq >/dev/null || { fail "jq is required"; return; }
  local section ref kind archs optional used found missing_arch a report hint
  while IFS='|' read -r section ref kind archs optional used; do
    [[ "${section}" == images ]] || continue
    found="$(image_archs "${ref}")"
    report=fail
    [[ "${optional}" == true ]] && report=warn
    if [[ "${found}" == MISSING:* && "${expect_unpublished}" == *" ${ref} "* ]]; then
      echo "note  ${ref} not published yet (expected: this release publishes it)"
      continue
    fi
    if [[ "${found}" == MISSING:* ]]; then
      hint="HTTP ${found#MISSING:}"
      [[ "${found}" == MISSING:401 ]] && hint+=": no anonymous pull, or the repo is gone"
      "${report}" "${ref} (${kind:-image}) not found on registry (${hint}); used in ${used//,/, }"
      continue
    fi
    missing_arch=""
    for a in ${archs//,/ }; do
      [[ ",${found}," == *",${a},"* ]] || missing_arch+="${a} "
    done
    if [[ -n "${missing_arch}" ]]; then
      "${report}" "${ref} lacks arch(s) ${missing_arch% } (registry has: ${found})"
    else
      ok "${ref} [${found}]"
    fi
    if [[ -n "${single_label}" ]]; then
      local key="${single_label%%=*}" want="${single_label#*=}" got
      got="$(image_label "${ref}" "${key}")"
      if [[ "${got}" == "${want}" ]]; then
        ok "${ref} label ${key}=${got}"
      else
        fail "${ref} label ${key}='${got}', expected '${want}'"
      fi
    fi
  done < <(images_to_check)
  return 0
}

images_to_check() {
  if [[ -n "${single_ref}" ]]; then
    printf 'images|%s|release|%s|false|--ref\n' "${single_ref}" "${single_archs}"
  else
    parse_images_file
  fi
}

# ---------------------------------------------------------------------------
# (b) Tree scan: every image ref in the tracked tree must be in images.yaml
# ---------------------------------------------------------------------------
REGISTRY_RE='(quay\.io|docker\.io|ghcr\.io|gcr\.io|registry\.access\.redhat\.com|registry\.redhat\.io|registry\.k8s\.io|public\.ecr\.aws|image-registry\.openshift-image-registry\.svc:5000)/[A-Za-z0-9._/-]+(:[A-Za-z0-9._-]+)?(@sha256:[a-f0-9]{64})?'

# Tracked files worth scanning. Docs, skills, CI config, tests, binaries and
# the release tooling itself are excluded: they mention images without
# pulling them.
scan_files() {
  git -C "${ROOT_DIR}" ls-files -z \
    | tr '\0' '\n' \
    | grep -vE '(^docs/|^\.claude/|^\.github/|^bin/|\.md$|\.pdf$|\.png$|\.tgz$|_test\.go$|(^|/)\.DS_Store$|^LICENSE$|^images\.yaml$|^scripts/(check-images|release-check|release-images|release-tag|bump-version|smoke)\.sh$)' \
    || true
}

# 1. Fully qualified refs anywhere (Go consts, scripts, manifests, env files).
scan_qualified() {
  local f
  while IFS= read -r f; do
    [[ -f "${ROOT_DIR}/${f}" ]] || continue
    grep -oE "${REGISTRY_RE}" "${ROOT_DIR}/${f}" 2>/dev/null \
      | grep -E '(:[A-Za-z0-9._-]+|@sha256:[a-f0-9]+)$' \
      | sed "s|\$|	${f}|" || true
  done
}

# 2. FROM lines in Dockerfiles/Containerfiles, including docker.io shorthand
#    (golang:1.25). ${VAR} is resolved from the file's own `ARG VAR=default`.
scan_from_lines() {
  local f
  while IFS= read -r f; do
    case "${f}" in
      *Dockerfile|*Containerfile|*.Dockerfile|*.dockerfile) ;;
      *) continue ;;
    esac
    [[ -f "${ROOT_DIR}/${f}" ]] || continue
    awk -v file="${f}" '
      toupper($1) == "ARG" { split($2, kv, "="); if (kv[2] != "") args[kv[1]] = kv[2] }
      toupper($1) == "FROM" {
        img = $2; if (img ~ /^--platform=/) img = $3
        if (toupper($3) == "AS") stages[tolower($4)] = 1
        if (toupper($4) == "AS") stages[tolower($5)] = 1
        for (k in args) { gsub("\\$\\{" k "\\}", args[k], img); gsub("\\$" k "$", args[k], img) }
        if (img == "scratch" || img ~ /^\$/ || (tolower(img) in stages)) next
        print img "\t" file
      }
    ' "${ROOT_DIR}/${f}"
  done
}

# 3. Split refs: Helm/Bitnami `image: {registry, repository, tag}` blocks in
#    YAML or JSON (values.yaml, CSV alm-examples, CR samples) and kustomize
#    `newName`/`newTag` pairs.
scan_split_refs() {
  local f
  while IFS= read -r f; do
    case "${f}" in *.yaml|*.yml|*.json) ;; *) continue ;; esac
    [[ -f "${ROOT_DIR}/${f}" ]] || continue
    awk -v file="${f}" '
      function val(s) { sub(/^[^:]*:[ \t]*/, "", s); gsub(/[",]/, "", s); sub(/[ \t]+#.*$/, "", s); gsub(/[ \t]/, "", s); return s }
      { line = $0; gsub(/"/, "", line) }
      line ~ /^[ \t]*image:[ \t]*\{?[ \t]*$/ { inblock = NR; reg = ""; repo = ""; next }
      line ~ /^[ \t]*-?[ \t]*newName:/ { nn = val(line); next }
      line ~ /^[ \t]*newTag:/ { if (nn != "") print nn ":" val(line) "\t" file; nn = ""; next }
      inblock && NR - inblock > 12 { inblock = 0 }
      inblock && line ~ /^[ \t]*registry:/   { reg = val(line); next }
      inblock && line ~ /^[ \t]*repository:/ { repo = val(line); next }
      inblock && line ~ /^[ \t]*tag:/ {
        t = val(line)
        if (repo != "" && t != "" && t !~ /\{\{/) print (reg != "" ? reg "/" : "") repo ":" t "\t" file
        inblock = 0
      }
    ' "${ROOT_DIR}/${f}"
  done
}

check_tree() {
  echo "== (b) tree: every image ref in the tracked tree is listed in images.yaml"
  local files known found ref file nref
  files="$(scan_files)"
  known="${TMP_DIR}/known"
  parse_images_file | while IFS='|' read -r _ ref _; do normalize_ref "${ref}"; done | sort -u >"${known}"

  found="${TMP_DIR}/found"
  {
    scan_qualified <<<"${files}"
    scan_from_lines <<<"${files}"
    scan_split_refs <<<"${files}"
  } | sort -u \
    | awk -F'\t' '{ files[$1] = ($1 in files) ? files[$1] ", " $2 : $2 } END { for (r in files) print r "\t" files[r] }' \
    | sort >"${found}"

  local unlisted=0 seen=0
  while IFS=$'\t' read -r ref file; do
    [[ -n "${ref}" ]] || continue
    seen=$((seen + 1))
    nref="$(normalize_ref "${ref}")"
    if ! grep -qxF "${nref}" "${known}"; then
      fail "${ref} in ${file} is not listed in images.yaml (add it under images: or allowlist:)"
      unlisted=$((unlisted + 1))
    elif [[ "${verbose}" == true ]]; then
      ok "${ref} (${file})"
    fi
  done <"${found}"
  if [[ "${unlisted}" -eq 0 ]]; then
    ok "${seen} image refs in the tree, all listed in images.yaml"
  fi

  # Stale entries: listed but no longer referenced anywhere. Warn only, since
  # some entries (the CLI image itself) are composed in code at runtime.
  local listed_only
  listed_only="$(cut -f1 "${found}" | while read -r r; do normalize_ref "${r}"; done | sort -u | comm -13 - "${known}")"
  if [[ -n "${listed_only}" && "${verbose}" == true ]]; then
    while IFS= read -r ref; do echo "note  ${ref} is listed in images.yaml but not found by the tree scan"; done <<<"${listed_only}"
  fi
  return 0
}

cd "${ROOT_DIR}"
if [[ "${do_scan}" == true ]]; then check_tree; fi
if [[ "${do_registry}" == true ]]; then check_registry; fi

echo
echo "check-images: ${failures} failure(s), ${warnings} warning(s)"
[[ "${failures}" -eq 0 ]]
