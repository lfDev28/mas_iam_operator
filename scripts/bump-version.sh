#!/usr/bin/env bash
# Rewrite every version pin of one mas-est artifact in one go.
#
#   scripts/bump-version.sh cli      0.1.16       # or: make bump ARTIFACT=cli VERSION=0.1.16
#   scripts/bump-version.sh cli      0.1.16-dev   # dev build: version.go only, docs stay on the last release
#   scripts/bump-version.sh bridge   0.1.3        # image tag scim-bridge-v0.1.3
#   scripts/bump-version.sh operator 0.0.16       # image 0.0.16, catalog-0.0.16, CSV v0.0.16
#
# Bump the operator and bridge BEFORE the CLI: the CLI bump adds a
# RELEASE-NOTES heading whose version line names the current bridge and
# operator pins. Never pushes, tags or commits; review with `git diff`.
# Versions are given without a leading "v". Release candidates (-rc.N) are
# not bumped here: version.go holds the final X.Y.Z and the rc suffix is
# stamped at image build time (see tools/mas-iam-installer/Containerfile).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

usage() { sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; }

artifact="${1:-}"
new="${2:-}"
new="${new#v}"
if [[ -z "${artifact}" || -z "${new}" || "${artifact}" == -h || "${artifact}" == --help ]]; then
  usage
  [[ "${artifact}" == -h || "${artifact}" == --help ]] && exit 0
  exit 2
fi

command -v perl >/dev/null || { echo "error: perl is required" >&2; exit 1; }

# perl, not sed -i: identical behaviour on macOS and Linux.
# subst <file> <perl-substitution>; fails if the file is missing.
subst() {
  local file="$1" expr="$2"
  [[ -f "${file}" ]] || { echo "error: ${file} not found" >&2; exit 1; }
  NEW="${new}" perl -pi -e "${expr}" "${file}"
}
# subst_if_present: for files another change may have removed.
subst_if_present() {
  [[ -f "$1" ]] && subst "$@"
  return 0
}

current_cli()      { sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' tools/mas-iam-installer/internal/version/version.go; }
current_bridge()   { sed -n 's/.*mas-iam-scim-bridge:scim-bridge-v\([0-9.]*\).*/\1/p' env/scim-bridge.env.release | head -1; }
current_operator() { sed -n 's/^VERSION ?= //p' operators/mas-iam-operator/Makefile; }

semver='^[0-9]+\.[0-9]+\.[0-9]+$'

case "${artifact}" in
  cli)
    [[ "${new}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-dev)?$ ]] || { echo "error: CLI version must be X.Y.Z or X.Y.Z-dev (got ${new})" >&2; exit 2; }
    old="$(current_cli)"
    subst tools/mas-iam-installer/internal/version/version.go \
      's/^(\s*Version\s*=\s*")[^"]*(")/${1}$ENV{NEW}${2}/'
    if [[ "${new}" == *-dev ]]; then
      echo "[bump] cli ${old} -> ${new} (dev build: version.go only; docs stay on the last release)"
    else
      # Release pins only: (?![-\w.]) leaves v0.1.0-beta.9 (the Containerfile
      # base) and any -dev/-rc tag alone.
      for f in README.md docs/GUIDE.md docs/ANNOUNCEMENT.md images.yaml; do
        subst "${f}" 's/(mas-external-services-tool:v)\d+\.\d+\.\d+(?![-\w.])/${1}$ENV{NEW}/g'
      done
      subst docs/ANNOUNCEMENT.md 's/^subtitle: "v[^"]*"/subtitle: "v$ENV{NEW}"/'
      if ! grep -q "^## v${new}\$" docs/RELEASE-NOTES.md; then
        line="CLI \`v${new}\` · bridge \`scim-bridge-v$(current_bridge)\` · operator \`$(current_operator)\`"
        NEW="${new}" LINE="${line}" perl -0pi -e 's/\n## /\n## v$ENV{NEW}\n\n$ENV{LINE}\n\n- **Fixed:** TODO symptom the user saw. One-line cause or change.\n\n## /' docs/RELEASE-NOTES.md
        echo "[bump] added a '## v${new}' skeleton to docs/RELEASE-NOTES.md; fill in the bullets"
      fi
      echo "[bump] cli ${old} -> ${new}"
      echo "[bump] next: update TROUBLESHOOTING / KNOWN-LIMITATIONS / GUIDE for what changed, then 'make docs-pdf'"
    fi
    ;;
  bridge)
    [[ "${new}" =~ ${semver} ]] || { echo "error: bridge version must be X.Y.Z (got ${new})" >&2; exit 2; }
    old="$(current_bridge)"
    # Match the full mas-iam-scim-bridge: repo so the mas-iam-keycloak image,
    # which shares the scim-bridge-vX.Y.Z tag naming, is never touched.
    expr='s/(mas-iam-scim-bridge:scim-bridge-v)\d+\.\d+\.\d+/${1}$ENV{NEW}/g'
    for f in env/scim-bridge.env.release scripts/install-all-in-one.sh manifests/scim-bridge-install.yaml images.yaml; do
      subst "${f}" "${expr}"
    done
    echo "[bump] bridge scim-bridge-v${old} -> scim-bridge-v${new}"
    echo "[bump] the image must be rebuilt: a bridge change without a new image is a no-op on the cluster"
    ;;
  operator)
    [[ "${new}" =~ ${semver} ]] || { echo "error: operator version must be X.Y.Z (got ${new})" >&2; exit 2; }
    old="$(current_operator)"
    subst operators/mas-iam-operator/Makefile 's/^VERSION \?= .*/VERSION ?= $ENV{NEW}/'
    subst operators/mas-iam-operator/config/manager/kustomization.yaml 's/^(\s*newTag:\s*).*/${1}$ENV{NEW}/'
    csv=operators/mas-iam-operator/bundle/manifests/mas-iam-operator.clusterserviceversion.yaml
    subst "${csv}" 's/^(  name: mas-iam-operator\.v)\d+\.\d+\.\d+$/${1}$ENV{NEW}/'
    subst "${csv}" 's/^(  version: )\d+\.\d+\.\d+$/${1}$ENV{NEW}/'
    for f in "${csv}" manifests/install-olm.yaml scripts/install-operator-from-catalog.sh images.yaml; do
      subst "${f}" 's/(mas-iam-operator:(?:catalog-)?)\d+\.\d+\.\d+(?![-\w.])/${1}$ENV{NEW}/g'
    done
    subst docs/GUIDE.md 's/(mas-iam-operator\.v)\d+\.\d+\.\d+/${1}$ENV{NEW}/g'
    echo "[bump] operator ${old} -> ${new} (image ${new}, catalog-${new}, CSV mas-iam-operator.v${new})"
    echo "[bump] publish the operator + bundle + catalog BEFORE any CLI release that ships these manifests"
    ;;
  *)
    echo "error: artifact must be cli, bridge or operator (got ${artifact})" >&2
    exit 2
    ;;
esac

echo
git --no-pager diff --stat
