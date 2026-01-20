#!/usr/bin/env bash

# Usage:
#   MAS_SCIM_BASE=https://api....../scim/v2 \
#   MAS_PROFILE_ID=test1 \
#   API_TOKEN_NAME=a-lfmas-... \
#   API_TOKEN_VALUE='...' \
#   ./scripts/scim-bridge-03-verify.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

: "${SCIM_BRIDGE_NAMESPACE:=iam}"
: "${MAS_SCIM_BASE:?Set MAS_SCIM_BASE (e.g., https://api....../scim/v2)}"
: "${MAS_PROFILE_ID:?Set MAS_PROFILE_ID (e.g., test1)}"
MAS_AUTH_SCHEME=${MAS_AUTH_SCHEME:-Bearer}
MAS_TOKEN=${MAS_TOKEN:-}
API_TOKEN_NAME=${API_TOKEN_NAME:-}
API_TOKEN_VALUE=${API_TOKEN_VALUE:-}

CURL_HELPER="$ROOT_DIR/scripts/mas-scim-curl.sh"

if [[ ! -x "$CURL_HELPER" ]]; then
  echo "mas-scim-curl.sh not found or not executable at $CURL_HELPER" >&2
  exit 1
fi

echo "[verify] deployment image/env (namespace: $SCIM_BRIDGE_NAMESPACE)"
oc get deploy/scim-bridge -n "$SCIM_BRIDGE_NAMESPACE" -o yaml | grep -E "image:|SCIM_BRIDGE_" || true

echo "[verify] recent logs"
oc logs deploy/scim-bridge -n "$SCIM_BRIDGE_NAMESPACE" --tail=100 || true

echo "[verify] MAS SCIM connectivity test via mas-scim-curl.sh"
if [[ -n "$MAS_TOKEN" ]]; then
  MAS_SCIM_BASE="$MAS_SCIM_BASE" MAS_PROFILE_ID="$MAS_PROFILE_ID" MAS_AUTH_SCHEME="$MAS_AUTH_SCHEME" MAS_TOKEN="$MAS_TOKEN" \
    "$CURL_HELPER" list
elif [[ -n "$API_TOKEN_NAME" && -n "$API_TOKEN_VALUE" ]]; then
  MAS_SCIM_BASE="$MAS_SCIM_BASE" MAS_PROFILE_ID="$MAS_PROFILE_ID" MAS_AUTH_SCHEME="$MAS_AUTH_SCHEME" \
    MAS_API_TOKEN_NAME="$API_TOKEN_NAME" MAS_API_TOKEN_VALUE="$API_TOKEN_VALUE" \
    "$CURL_HELPER" list
else
  echo "set MAS_TOKEN or API_TOKEN_NAME/API_TOKEN_VALUE to run the SCIM list call" >&2
  exit 1
fi
