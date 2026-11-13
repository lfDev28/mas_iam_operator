#!/usr/bin/env bash
# Pre-release gate. Read-only: never builds, pushes, tags or commits.
#
#   scripts/release-check.sh --version 0.1.16 [--operator-version 0.0.16] [--bridge-version 0.1.3]
#   make release-check VERSION=0.1.16 [OPERATOR_VERSION=0.0.16] [BRIDGE_VERSION=0.1.3]
#
# --version is the final CLI version (X.Y.Z, no "v", no -rc: the rc suffix is
# only stamped into the image at build time). Operator and bridge versions
# default to what the repo pins today.
#
# Checks, in order (every check runs; the exit status is 1 if any failed):
#   1. the working tree is clean
#   2. version.go and every doc / manifest / script / images.yaml pin match
#   3. docs/RELEASE-NOTES.md has '## vX.Y.Z', its version line names all
#      three artifacts, and it has no TODO left
#   4. operator and bridge sources have not changed since their last release
#      tag (operator-0.0.N, scim-bridge-vX.Y.Z) unless their version was bumped
#   5. check-images passes (the CLI tag being released may not exist yet;
#      the operator and bridge images it references must)
#   6. make check passes
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

usage() { sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; }

version=""
operator_version=""
bridge_version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version="${2:-}"; shift 2 ;;
    --operator-version) operator_version="${2:-}"; shift 2 ;;
    --bridge-version) bridge_version="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
version="${version#v}"
bridge_version="${bridge_version#scim-bridge-}"; bridge_version="${bridge_version#v}"
operator_version="${operator_version#v}"

semver='^[0-9]+\.[0-9]+\.[0-9]+$'
if [[ ! "${version}" =~ ${semver} ]]; then
  echo "error: --version X.Y.Z is required (got '${version}'). Pass the final version, not -rc.N or -dev." >&2
  exit 2
fi

failures=0
warnings=0
fail() { echo "FAIL  $*"; failures=$((failures + 1)); }
warn() { echo "WARN  $*"; warnings=$((warnings + 1)); }
ok()   { echo "ok    $*"; }
section() { echo; echo "== $*"; }

repo_cli()      { sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' tools/mas-iam-installer/internal/version/version.go; }
repo_bridge()   { sed -n 's/.*mas-iam-scim-bridge:scim-bridge-v\([0-9.]*\).*/\1/p' env/scim-bridge.env.release | head -1; }
repo_operator() { sed -n 's/^VERSION ?= //p' operators/mas-iam-operator/Makefile; }

operator_version="${operator_version:-$(repo_operator)}"
bridge_version="${bridge_version:-$(repo_bridge)}"
echo "release-check: CLI v${version} · bridge scim-bridge-v${bridge_version} · operator ${operator_version}"

# expect_pins <label> <expected> <git-grep -E pattern> <pathspec...>
# Every match of the pattern in the tracked tree must equal <expected>.
expect_pins() {
  local label="$1" expected="$2" pattern="$3"; shift 3
  local matches bad count
  matches="$(git grep -noE "${pattern}" -- "$@" 2>/dev/null || true)"
  count="$(grep -c . <<<"${matches}" || true)"
  if [[ -z "${matches}" ]]; then
    fail "${label}: no pins found (pattern ${pattern})"
    return
  fi
  bad="$(awk -F: -v want="${expected}" '{ v = $0; sub(/^[^:]*:[^:]*:/, "", v); if (v != want) print "      " $0 }' <<<"${matches}")"
  if [[ -n "${bad}" ]]; then
    fail "${label}: expected ${expected} everywhere; these differ:"
    echo "${bad}"
  else
    ok "${label}: ${count} pin(s) = ${expected}"
  fi
}

# Paths never treated as pins: history, design records, skills, tests, PDFs.
EXCLUDES=(':!docs/RELEASE-NOTES.md' ':!docs/design' ':!.claude' ':!*_test.go' ':!*.pdf' ':!bin')

# ---------------------------------------------------------------------------
section "1. working tree"
if [[ -n "$(git status --porcelain)" ]]; then
  fail "working tree is not clean; commit or stash first:"
  git status --short | sed 's/^/      /'
else
  ok "clean at $(git rev-parse --short HEAD) ($(git rev-parse --abbrev-ref HEAD))"
fi

# ---------------------------------------------------------------------------
section "2. version pins"
if [[ "$(repo_cli)" == "${version}" ]]; then
  ok "version.go Version = ${version}"
else
  fail "version.go Version = $(repo_cli), expected ${version} (make bump ARTIFACT=cli VERSION=${version})"
fi
# Release pins of the CLI image. The Containerfile's v0.1.0-beta.9 runtime
# base is a separate, deliberate pin and is excluded.
expect_pins "CLI image pins" "mas-external-services-tool:v${version}" \
  'mas-external-services-tool:v[0-9][0-9A-Za-z.-]*' . "${EXCLUDES[@]}" \
  ':!tools/mas-iam-installer/Containerfile' ':!images.yaml'
expect_pins "CLI image in images.yaml" "mas-external-services-tool:v${version}" \
  'mas-external-services-tool:v[0-9]+\.[0-9]+\.[0-9]+([^-.0-9A-Za-z]|$)' images.yaml
if grep -q "^subtitle: \"v${version}\"" docs/ANNOUNCEMENT.md; then
  ok "docs/ANNOUNCEMENT.md subtitle = v${version}"
else
  fail "docs/ANNOUNCEMENT.md subtitle is not \"v${version}\""
fi
if grep -qF "\`version\` must print \`${version}\`" docs/GUIDE.md; then
  ok "docs/GUIDE.md bootstrap check prints ${version}"
else
  fail "docs/GUIDE.md: the bootstrap step's \"\`version\` must print\" line is not ${version} (make bump ARTIFACT=cli updates it)"
fi

expect_pins "bridge image pins" "mas-iam-scim-bridge:scim-bridge-v${bridge_version}" \
  'mas-iam-scim-bridge:scim-bridge-v[0-9][0-9A-Za-z.-]*' . "${EXCLUDES[@]}"

# The operator pins come in several shapes; check each explicitly.
op_bad=0
while IFS= read -r line; do
  [[ -n "${line}" ]] || continue
  value="${line#*:*:}"
  case "${value}" in
    "mas-iam-operator:${operator_version}"|"mas-iam-operator:catalog-${operator_version}"|"mas-iam-operator.v${operator_version}") ;;
    *) [[ "${op_bad}" -eq 0 ]] && fail "operator pins: expected ${operator_version} everywhere; these differ:"; op_bad=1; echo "      ${line}" ;;
  esac
done < <(git grep -noE 'mas-iam-operator(:catalog-|:|\.v)[0-9]+\.[0-9]+\.[0-9]+' -- . "${EXCLUDES[@]}" \
           ':!operators/mas-iam-operator/config/manifests/bases' || true)
for spec in \
  "operators/mas-iam-operator/Makefile|^VERSION \?= ${operator_version}\$" \
  "operators/mas-iam-operator/config/manager/kustomization.yaml|^[[:space:]]*newTag: ${operator_version}\$" \
  "operators/mas-iam-operator/bundle/manifests/mas-iam-operator.clusterserviceversion.yaml|^  version: ${operator_version}\$"; do
  file="${spec%%|*}"; re="${spec#*|}"
  if ! grep -Eq "${re}" "${file}"; then
    [[ "${op_bad}" -eq 0 ]] && fail "operator pins: expected ${operator_version} everywhere; these differ:"
    op_bad=1
    echo "      ${file}: no line matching /${re}/"
  fi
done
[[ "${op_bad}" -eq 0 ]] && ok "operator pins (Makefile, kustomization, CSV, install-olm, catalog script, GUIDE) = ${operator_version}"

# ---------------------------------------------------------------------------
section "3. release notes"
notes=docs/RELEASE-NOTES.md
if grep -q "^## v${version}\$" "${notes}"; then
  body="$(awk -v h="## v${version}" '$0 == h { on = 1; next } on && /^## / { exit } on' "${notes}")"
  want_line="CLI \`v${version}\` · bridge \`scim-bridge-v${bridge_version}\` · operator \`${operator_version}\`"
  if grep -qF "${want_line}" <<<"${body}"; then
    ok "'## v${version}' with version line: ${want_line}"
  else
    fail "'## v${version}' needs the version line: ${want_line}"
  fi
  if grep -q 'TODO' <<<"${body}"; then
    fail "'## v${version}' still contains TODO"
  fi
  if ! grep -q '^- ' <<<"${body}"; then
    fail "'## v${version}' has no bullets"
  fi
else
  fail "${notes} has no '## v${version}' heading (make bump ARTIFACT=cli adds a skeleton)"
fi
# PDFs are committed next to their Markdown; flag any that were not re-rendered.
for md in docs/ANNOUNCEMENT.md docs/GUIDE.md docs/TROUBLESHOOTING.md docs/KNOWN-LIMITATIONS.md docs/RELEASE-NOTES.md; do
  pdf="${md%.md}.pdf"
  git ls-files --error-unmatch "${pdf}" >/dev/null 2>&1 || continue
  md_time="$(git log -1 --format=%ct -- "${md}")"
  pdf_time="$(git log -1 --format=%ct -- "${pdf}")"
  if [[ -n "${md_time}" && -n "${pdf_time}" && "${md_time}" -gt "${pdf_time}" ]]; then
    warn "${pdf} is older than ${md}; run 'make docs-pdf' and commit"
  fi
done

# ---------------------------------------------------------------------------
section "4. operator / bridge changed since their last release?"
latest_tag() { # latest_tag <prefix>: highest <prefix>X.Y.Z tag, or empty
  git tag -l "$1*" | sed "s/^$1//" | grep -E "${semver}" | sort -t. -k1,1n -k2,2n -k3,3n | tail -1 || true
}
# component_check <name> <tag-prefix> <version> <image-ref> <pin-pickaxe> <pin-file> <bump-hint> <paths...>
#   tag exists            -> any change to <paths> since the tag fails (needs a bump)
#   no tag, image on Quay -> shipped without a git tag: warn, say how to tag
#                            it, and diff against the commit that set the pin
#   no tag, no image      -> a new version, to publish before the CLI rc
component_check() {
  local name="$1" prefix="$2" target="$3" image="$4" pickaxe="$5" pin_file="$6" bump="$7"; shift 7
  local tag="${prefix}${target}" sha last
  if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
    if git diff --quiet "${tag}" HEAD -- "$@"; then
      ok "${name}: unchanged since ${tag}"
    else
      fail "${name}: sources changed since ${tag} shipped, but the version is still ${target}. ${bump}, then publish the image:"
      git --no-pager diff --stat "${tag}" HEAD -- "$@" | sed 's/^/      /'
    fi
    return
  fi
  if ./scripts/check-images.sh --ref "${image}" >/dev/null 2>&1; then
    sha="$(git log -1 --format=%h -S"${pickaxe}" -- "${pin_file}" 2>/dev/null || true)"
    warn "${name} ${target} is published but has no git tag ${tag}, so changes since it shipped cannot be checked exactly."
    echo "      Tag the commit it was built from (best guess: ${sha:-unknown}, the commit that set the pin):"
    echo "        git tag ${tag} ${sha:-<commit>} && git push origin ${tag}"
    if [[ -n "${sha}" ]] && ! git diff --quiet "${sha}" HEAD -- "$@"; then
      warn "${name}: sources changed since ${sha}. If any of it changes behaviour: ${bump}, then publish the image:"
      git --no-pager diff --stat "${sha}" HEAD -- "$@" | sed 's/^/      /'
    fi
  else
    last="$(latest_tag "${prefix}")"
    ok "${name} ${target} is a new version (last tag: ${last:+${prefix}}${last:-none}); publish it before the CLI rc"
  fi
}
component_check operator operator- "${operator_version}" "quay.io/lee_forster/mas-iam-operator:${operator_version}" \
  "VERSION ?= ${operator_version}" operators/mas-iam-operator/Makefile "Run make bump ARTIFACT=operator VERSION=X.Y.Z" \
  operators/mas-iam-operator/helm-charts operators/mas-iam-operator/watches.yaml operators/mas-iam-operator/Dockerfile ':!*.md'
component_check "SCIM bridge" scim-bridge-v "${bridge_version}" "quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-v${bridge_version}" \
  "scim-bridge-v${bridge_version}" env/scim-bridge.env.release "Run make bump ARTIFACT=bridge VERSION=X.Y.Z" \
  services/scim-bridge ':!services/scim-bridge/.DS_Store' ':!*.md'

# ---------------------------------------------------------------------------
section "5. check-images"
# The CLI tag is what this release publishes, so it may not exist yet. The
# operator and bridge tags must: the CLI ships manifests that pull them.
log="$(mktemp)"
if ./scripts/check-images.sh --expect-unpublished "quay.io/lee_forster/mas-external-services-tool:v${version}" >"${log}" 2>&1; then
  grep -vE '^ok    ' "${log}" | sed 's/^/      /'
  ok "check-images"
else
  grep -vE '^ok    ' "${log}" | sed 's/^/      /'
  fail "check-images (above). A missing operator/catalog/bridge tag means: publish that artifact first."
fi
rm -f "${log}"

# ---------------------------------------------------------------------------
section "6. make check"
log="$(mktemp)"
if make check >"${log}" 2>&1; then
  ok "make check"
else
  fail "make check; last lines:"
  tail -20 "${log}" | sed 's/^/      /'
fi
rm -f "${log}"

echo
echo "release-check: ${failures} failure(s), ${warnings} warning(s)"
if [[ "${failures}" -eq 0 ]]; then
  echo "Next: make release-images ARTIFACT=cli VERSION=${version}-rc.1 PUSH=1 (needs named approval), then make smoke."
fi
[[ "${failures}" -eq 0 ]]
