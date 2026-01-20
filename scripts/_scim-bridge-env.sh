#!/usr/bin/env bash
# Source this from other scripts to load env/scim-bridge.env.local if present.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE_DEFAULT="$ROOT_DIR/env/scim-bridge.env.local"
ENV_FILE="${SCIM_BRIDGE_ENV_FILE:-$ENV_FILE_DEFAULT}"

if [[ -f "$ENV_FILE" ]]; then
  while IFS= read -r line; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    [[ "$line" != *"="* ]] && continue
    key="${line%%=*}"
    val="${line#*=}"
    key="$(echo "$key" | sed 's/^ *//;s/ *$//')"
    val="$(echo "$val" | sed 's/^ *//;s/ *$//')"
    [[ -z "$key" ]] && continue
    if [[ -z "${!key-}" ]]; then
      export "$key"="$val"
    fi
  done <"$ENV_FILE"
fi
