#!/usr/bin/env bash
# Last step of a release: verify what is published, then create the git tags.
#
#   scripts/release-tag.sh --version 0.1.16 [--operator-version 0.0.16] [--bridge-version 0.1.3] [--push]
#   make release-tag VERSION=0.1.16 [OPERATOR_VERSION=..] [BRIDGE_VERSION=..] [PUSH=1]
#
# Tags are created only after the images they name are verified on Quay,
# because `mas-est update` and scripts/get-mas-est.sh install the highest
# vX.Y.Z image tag: a git tag must never point at an untested build.
#   1. the tree is clean
#   2. the CLI image vX.Y.Z is on Quay, multi-arch, and its
#      org.opencontainers.image.revision label is this commit (so the
#      promoted image is the same commit the rc was built from)
#   3. the operator / bridge images for the given (or pinned) versions exist
#   4. annotated tags vX.Y.Z, operator-N, scim-bridge-vB are created here
#      (any that already exist are left alone)
#   5. with --push: pushes the current branch and those tags to origin
# Without --push it prints the push command instead.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

usage() { sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; }

version=""; operator_version=""; bridge_version=""; push=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version="${2:-}"; shift 2 ;;
    --operator-version) operator_version="${2:-}"; shift 2 ;;
    --bridge-version) bridge_version="${2:-}"; shift 2 ;;
    --push) push=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
version="${version#v}"
[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "error: --version X.Y.Z (final, no -rc) is required" >&2; exit 2; }
operator_version="${operator_version:-$(sed -n 's/^VERSION ?= //p' operators/mas-iam-operator/Makefile)}"
bridge_version="${bridge_version:-$(sed -n 's/.*mas-iam-scim-bridge:scim-bridge-v\([0-9.]*\).*/\1/p' env/scim-bridge.env.release | head -1)}"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "error: working tree is not clean" >&2
  exit 1
fi
commit="$(git rev-parse --short=12 HEAD)"
branch="$(git rev-parse --abbrev-ref HEAD)"
[[ "${branch}" == main ]] || echo "warning: tagging on branch '${branch}', not main"

echo "== verify published images (commit ${commit})"
./scripts/check-images.sh --ref "quay.io/lee_forster/mas-external-services-tool:v${version}" \
  --archs amd64,arm64 --label "org.opencontainers.image.revision=${commit}"
./scripts/check-images.sh --ref "quay.io/lee_forster/mas-iam-operator:${operator_version}" --archs amd64
./scripts/check-images.sh --ref "quay.io/lee_forster/mas-iam-operator:catalog-${operator_version}" --archs amd64
./scripts/check-images.sh --ref "quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-v${bridge_version}" --archs amd64

echo "== tags"
created=()
for tag in "v${version}" "operator-${operator_version}" "scim-bridge-v${bridge_version}"; do
  if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
    echo "exists  ${tag} -> $(git rev-list -n1 --abbrev-commit "${tag}") (left alone)"
  else
    git tag -a "${tag}" -m "${tag}" HEAD
    echo "created ${tag} -> ${commit}"
    created+=("${tag}")
  fi
done

if [[ "${#created[@]}" -eq 0 ]]; then
  echo "nothing new to push"
  exit 0
fi
if [[ "${push}" == true ]]; then
  git push origin "${branch}" "${created[@]}"
else
  echo
  echo "Not pushed. When the user approves, push the branch and the new tags last:"
  echo "  git push origin ${branch} ${created[*]}"
fi
