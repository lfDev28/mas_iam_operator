#!/usr/bin/env bash
# Install or refresh the local mas-est runtime from the newest release on Quay.
#
#   scripts/get-mas-est.sh            # newest vX.Y.Z release
#   scripts/get-mas-est.sh v0.1.13    # a specific tag
#
# MAS_EST_DIR overrides the install directory (default ~/mas-est).
set -euo pipefail

REPOSITORY="quay.io/lee_forster/mas-external-services-tool"
TAGS_URL="https://quay.io/api/v1/repository/lee_forster/mas-external-services-tool/tag/?onlyActiveTags=true&limit=100"
DIR="${MAS_EST_DIR:-$HOME/mas-est}"
TAG="${1:-}"

if command -v podman >/dev/null 2>&1; then
  ENGINE=podman
elif command -v docker >/dev/null 2>&1; then
  ENGINE=docker
else
  echo "error: podman or docker is required" >&2
  exit 1
fi

if [[ -z "${TAG}" ]]; then
  # Release tags only: skips -dev, -beta.N and the per-arch -amd64/-arm64 tags.
  TAG="$(curl -fsSL "${TAGS_URL}" \
    | grep -oE '"name": *"v[0-9]+\.[0-9]+\.[0-9]+"' \
    | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' \
    | sort -t. -k1,1n -k2,2n -k3,3n \
    | tail -1)"
  if [[ -z "${TAG}" ]]; then
    echo "error: no release tag found at ${TAGS_URL}" >&2
    exit 1
  fi
  TAG="v${TAG}"
fi

IMAGE="${REPOSITORY}:${TAG}"
echo "[get-mas-est] installing ${IMAGE} into ${DIR}"
mkdir -p "${DIR}"
"${ENGINE}" run --rm -v "${DIR}:/tmp" --pull always "${IMAGE}" bootstrap --force

echo
"${DIR}/mas-est" version
cat <<EOF

[get-mas-est] done. In your shell, run:

  export MAS_EST_IMAGE='${IMAGE}'
  export PATH="${DIR}:\$PATH"
EOF
