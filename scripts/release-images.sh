#!/usr/bin/env bash
# Build, push and verify the images of one mas-est artifact.
#
#   scripts/release-images.sh cli      0.1.16-rc.1            # build only (local, both archs)
#   scripts/release-images.sh cli      0.1.16-rc.1 --push     # build, push, manifest list, verify
#   scripts/release-images.sh cli      0.1.16      --push     # promotion: same commit, final tag
#   scripts/release-images.sh bridge   0.1.3       --push
#   scripts/release-images.sh operator 0.0.16      --push     # operator + bundle + catalog
#   scripts/release-images.sh <artifact> <version> --verify   # registry check only, no podman
#
#   make release-images ARTIFACT=cli VERSION=0.1.16-rc.1 PUSH=1
#   make release-verify ARTIFACT=cli VERSION=0.1.16-rc.1
#
# --push needs the user's explicit, version-specific approval (see the
# release skill) and `podman login quay.io`. Every build forces --platform:
# on an Apple Silicon Mac an unforced build publishes arm64 images that
# crash-loop on the amd64 cluster with an exec-format error.
#
# Refuses to: build from a dirty tree; push a version the repo pins
# disagree with; overwrite an existing release or rc tag (published tags are
# immutable in practice; -dev tags may be re-pushed).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

QUAY=quay.io/lee_forster
CLI_REPO="${QUAY}/mas-external-services-tool"
BRIDGE_REPO="${QUAY}/mas-iam-scim-bridge"
OPERATOR_REPO="${QUAY}/mas-iam-operator"
BUNDLE_REPO="${QUAY}/mas-iam-operator-bundle"
ENGINE="${CONTAINER_ENGINE:-podman}"

usage() { sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; }

artifact="${1:-}"; version="${2:-}"; mode="${3:-build}"
case "${artifact}" in -h|--help|"") usage; [[ -n "${artifact}" ]] && exit 0; exit 2 ;; esac
version="${version#v}"
case "${mode}" in build|--push|--verify) ;; *) echo "error: unknown mode ${mode}" >&2; exit 2 ;; esac
[[ -n "${version}" ]] || { echo "error: version is required" >&2; exit 2; }

run() { echo "+ $*"; "$@"; }
check_ref() { ./scripts/check-images.sh --ref "$@"; }
exists() { ./scripts/check-images.sh --ref "$1" >/dev/null 2>&1; }

commit="$(git rev-parse --short=12 HEAD)"
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

preflight() {
  if [[ -n "$(git status --porcelain)" ]]; then
    echo "error: working tree is not clean; images are stamped with the commit, so commit first" >&2
    exit 1
  fi
  command -v "${ENGINE}" >/dev/null || { echo "error: ${ENGINE} not found" >&2; exit 1; }
  if [[ "${mode}" == --push ]]; then
    if ! "${ENGINE}" login --get-login quay.io >/dev/null 2>&1; then
      echo "error: not logged in to quay.io; run '${ENGINE} login quay.io' yourself, then re-run" >&2
      exit 1
    fi
  fi
}

refuse_overwrite() { # refuse_overwrite <ref>: release and rc tags are immutable
  if [[ "${mode}" == --push && "$1" != *-dev* ]] && exists "$1"; then
    echo "error: $1 already exists on the registry. Published tags are immutable: cut a new version (or rc number)." >&2
    exit 1
  fi
}

repo_cli()      { sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' tools/mas-iam-installer/internal/version/version.go; }
repo_bridge()   { sed -n 's/.*mas-iam-scim-bridge:scim-bridge-v\([0-9.]*\).*/\1/p' env/scim-bridge.env.release | head -1; }
repo_operator() { sed -n 's/^VERSION ?= //p' operators/mas-iam-operator/Makefile; }

# ---------------------------------------------------------------------------
cli() {
  local tag="v${version}" img="${CLI_REPO}:v${version}" arch
  # Same rule as the Containerfile gate, checked up front to fail fast.
  case "${version}" in
    "$(repo_cli)"|"$(repo_cli)"-rc.[0-9]*) ;;
    *) echo "error: CLI ${version} does not match version.go ($(repo_cli)) or its -rc.N" >&2; exit 1 ;;
  esac
  if [[ "${mode}" == --verify ]]; then
    check_ref "${img}" --archs amd64,arm64 --label "org.opencontainers.image.revision=${commit}"
    check_ref "${img}-amd64" --archs amd64
    check_ref "${img}-arm64" --archs arm64
    return
  fi
  preflight
  refuse_overwrite "${img}"
  # The build context is the repo root: the image bundles scripts/,
  # manifests/ and env/.
  for arch in amd64 arm64; do
    run "${ENGINE}" build --platform "linux/${arch}" -f tools/mas-iam-installer/Containerfile \
      --build-arg "VERSION=${version}" --build-arg "COMMIT=${commit}" --build-arg "BUILD_DATE=${build_date}" \
      -t "${img}-${arch}" .
  done
  if [[ "${mode}" != --push ]]; then
    echo "[release-images] built ${img}-amd64 and -arm64 locally; nothing pushed (add --push)"
    return
  fi
  run "${ENGINE}" push "${img}-amd64"
  run "${ENGINE}" push "${img}-arm64"
  # A plain local image under the manifest name (left by any earlier pull or
  # run of this tag) makes `manifest create` fail with "image is not a
  # manifest list" while the per-arch pushes succeed, so the multi-arch tag
  # silently keeps pointing at the previous build. Clear both first.
  "${ENGINE}" rmi -f "${img}" >/dev/null 2>&1 || true
  "${ENGINE}" manifest rm "${img}" >/dev/null 2>&1 || true
  run "${ENGINE}" manifest create "${img}"
  run "${ENGINE}" manifest add "${img}" "docker://${img}-amd64"
  run "${ENGINE}" manifest add "${img}" "docker://${img}-arm64"
  run "${ENGINE}" manifest push --all "${img}" "docker://${img}"
  echo "[release-images] verifying ${tag} from the registry (never trust the push output)"
  mode=--verify cli
}

# ---------------------------------------------------------------------------
bridge() {
  local img="${BRIDGE_REPO}:scim-bridge-v${version}"
  [[ "${version}" == "$(repo_bridge)" ]] || { echo "error: bridge ${version} != repo pin $(repo_bridge); run make bump ARTIFACT=bridge first" >&2; exit 1; }
  if [[ "${mode}" == --verify ]]; then
    check_ref "${img}" --archs amd64
    return
  fi
  preflight
  refuse_overwrite "${img}"
  # Always amd64: the bridge only runs on the cluster. (The old build script
  # inferred the arch from whatever cluster oc pointed at.)
  run "${ENGINE}" build --platform linux/amd64 -f services/scim-bridge/Dockerfile \
    --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t "${img}" services/scim-bridge
  if [[ "${mode}" != --push ]]; then
    echo "[release-images] built ${img} locally; nothing pushed (add --push)"
    return
  fi
  run "${ENGINE}" push "${img}"
  mode=--verify bridge
}

# ---------------------------------------------------------------------------
operator() {
  local img="${OPERATOR_REPO}:${version}" bundle="${BUNDLE_REPO}:${version}" catalog="${OPERATOR_REPO}:catalog-${version}"
  [[ "${version}" == "$(repo_operator)" ]] || { echo "error: operator ${version} != repo pin $(repo_operator); run make bump ARTIFACT=operator first" >&2; exit 1; }
  if [[ "${mode}" == --verify ]]; then
    check_ref "${img}" --archs amd64
    check_ref "${bundle}"
    check_ref "${catalog}" --archs amd64
    return
  fi
  preflight
  refuse_overwrite "${img}"
  refuse_overwrite "${catalog}"
  (
    cd operators/mas-iam-operator
    # Regenerate the bundle first and stop if it differs from what is
    # committed (ignoring operator-sdk's createdAt stamp): the bundle image
    # must be built from committed manifests.
    run env CONTAINER_ENGINE="${ENGINE}" VERSION="${version}" IMG="${img}" make bundle
    if ! git diff --quiet -I'createdAt:' -- bundle config; then
      git --no-pager diff --stat -- bundle config
      echo "error: 'make bundle' changed the committed bundle; review, commit, and re-run" >&2
      exit 1
    fi
    git checkout -- bundle config

    run "${ENGINE}" build --platform linux/amd64 -t "${img}" .
    run "${ENGINE}" build --platform linux/amd64 -f bundle.Dockerfile -t "${bundle}" .
    if [[ "${mode}" != --push ]]; then
      echo "[release-images] built ${img} and ${bundle} locally; the catalog needs the bundle pushed (add --push)"
      exit 0
    fi
    run "${ENGINE}" push "${img}"
    run "${ENGINE}" push "${bundle}"
    run make opm
    # --generate emits index.Dockerfile instead of building, which is the only
    # way to force the catalog image's arch. The tag is
    # mas-iam-operator:catalog-N, not the Makefile's CATALOG_IMG default.
    run ./bin/opm index add --container-tool "${ENGINE}" --mode semver \
      --tag "${catalog}" --bundles "${bundle}" --generate --out-dockerfile index.Dockerfile
    run "${ENGINE}" build --platform linux/amd64 -f index.Dockerfile -t "${catalog}" .
    run "${ENGINE}" push "${catalog}"
    git checkout -- index.Dockerfile 2>/dev/null || true
    rm -rf database
  )
  [[ "${mode}" == --push ]] || return 0
  mode=--verify operator
  echo "[release-images] on a cluster, confirm the CatalogSource is READY and packagemanifests reports currentCSV mas-iam-operator.v${version} (scripts/smoke.sh does)"
}

case "${artifact}" in
  cli) cli ;;
  bridge) bridge ;;
  operator) operator ;;
  *) echo "error: artifact must be cli, bridge or operator" >&2; exit 2 ;;
esac
