#!/usr/bin/env bash

# Usage:
#   SCIM_BRIDGE_IMAGE=quay.io/<org>/scim-bridge:dev ./scripts/scim-bridge-01-build-image.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

: "${SCIM_BRIDGE_IMAGE:?Set SCIM_BRIDGE_IMAGE to <registry>/scim-bridge:tag}"
: "${SCIM_BRIDGE_CONTEXT:=services/scim-bridge}"

SCIM_BRIDGE_TARGET_OS=${SCIM_BRIDGE_TARGET_OS:-linux}
if [[ -z "${SCIM_BRIDGE_TARGET_ARCH:-}" ]]; then
  detected_arch="amd64"
  if command -v oc >/dev/null 2>&1; then
    node_arch=$(oc get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}' 2>/dev/null || true)
    if [[ -n "$node_arch" ]]; then
      detected_arch="$node_arch"
    fi
  fi
  case "$detected_arch" in
    x86_64) SCIM_BRIDGE_TARGET_ARCH=amd64 ;;
    aarch64) SCIM_BRIDGE_TARGET_ARCH=arm64 ;;
    *) SCIM_BRIDGE_TARGET_ARCH="$detected_arch" ;;
  esac
fi

CONTAINER_CLI=${SCIM_BRIDGE_CONTAINER_CLI:-}
if [[ -z "$CONTAINER_CLI" ]]; then
  if command -v docker >/dev/null 2>&1; then
    CONTAINER_CLI="docker"
  elif command -v podman >/dev/null 2>&1; then
    CONTAINER_CLI="podman"
  else
    echo "neither docker nor podman found; set SCIM_BRIDGE_CONTAINER_CLI explicitly" >&2
    exit 1
  fi
fi

REPO_ROOT="$ROOT_DIR"
cd "$REPO_ROOT"

echo "[scim-bridge] building image: $SCIM_BRIDGE_IMAGE"
echo "[scim-bridge] build context: $SCIM_BRIDGE_CONTEXT"
echo "[scim-bridge] container cli: $CONTAINER_CLI"
echo "[scim-bridge] target platform: ${SCIM_BRIDGE_TARGET_OS}/${SCIM_BRIDGE_TARGET_ARCH}"

$CONTAINER_CLI build \
  --platform="${SCIM_BRIDGE_TARGET_OS}/${SCIM_BRIDGE_TARGET_ARCH}" \
  -t "$SCIM_BRIDGE_IMAGE" \
  -f "$SCIM_BRIDGE_CONTEXT/Dockerfile" \
  --build-arg TARGETOS="$SCIM_BRIDGE_TARGET_OS" \
  --build-arg TARGETARCH="$SCIM_BRIDGE_TARGET_ARCH" \
  "$SCIM_BRIDGE_CONTEXT"
$CONTAINER_CLI push "$SCIM_BRIDGE_IMAGE"
