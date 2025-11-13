#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_SYMLINK="${ROOT_DIR}/charts/mas-iam-stack"
CANONICAL_CHART="${ROOT_DIR}/operators/mas-iam-operator/helm-charts/mas-iam-stack"

if [[ ! -d "${CANONICAL_CHART}" ]]; then
  echo "error: canonical chart missing at ${CANONICAL_CHART}" >&2
  exit 1
fi

if [[ ! -L "${CHART_SYMLINK}" ]]; then
  echo "error: ${CHART_SYMLINK} must remain a symlink to ${CANONICAL_CHART}" >&2
  exit 1
fi

resolved_link="$(cd "${CHART_SYMLINK}" && pwd -P)"
resolved_canonical="$(cd "${CANONICAL_CHART}" && pwd -P)"

if [[ "${resolved_link}" != "${resolved_canonical}" ]]; then
  echo "error: ${CHART_SYMLINK} points to ${resolved_link}, expected ${resolved_canonical}" >&2
  exit 1
fi

echo "Helm chart is single-sourced at ${resolved_canonical}."
