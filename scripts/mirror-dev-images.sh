#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: mirror-dev-images.sh [--dest <registry/path>] [--dry-run]

Mirrors the upstream OpenLDAP and PostgreSQL container images to a target
registry while preserving the full multi-architecture manifest list (amd64 and
arm64). This keeps the MAS IAM development manifests pinned to a single
registry (default: quay.io/lee_forster/mas-iam-operator) without breaking on
clusters whose nodes have a different CPU architecture.

Prerequisites:
  * oc CLI 4.10+ (for `oc image mirror`)
  * Credentials for both source (Docker Hub) and destination registries
    configured via `podman login`/`docker login`

Options:
  --dest <registry/path>  Destination repository (default:
                          quay.io/lee_forster/mas-iam-operator)
  --dry-run               Print the mirror operations without executing them
  -h, --help              Show this message
EOF
}

dest_repo="quay.io/lee_forster/mas-iam-operator"
dry_run=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dest)
      dest_repo="${2-}"
      shift 2
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "${dest_repo}" ]]; then
  echo "error: destination repository must not be empty" >&2
  exit 1
fi

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI is required on PATH" >&2
  exit 1
fi

images=(
  # Bitnami periodically prunes versioned tags from Docker Hub. Pin to the
  # manifest-list digest published on 2025-11-08 so oc can still pull it.
  "postgresql-17.6.0-debian-12-r4=docker.io/bitnami/postgresql@sha256:14aa30df4c41d43590a0ae59b1963522f5e4344bedda67e1f2e423ea8267c115"
  "openldap-1.5.0=docker.io/osixia/openldap:1.5.0"
)

for mapping in "${images[@]}"; do
  tag="${mapping%%=*}"
  src="${mapping##*=}"
  dest="${dest_repo}:${tag}"

  echo "Mirroring ${src} -> ${dest}"
  if [[ "${dry_run}" == "true" ]]; then
    continue
  fi

  # --keep-manifest-list preserves the multi-arch index from the upstream image.
  oc image mirror --keep-manifest-list "${src}" "${dest}"
done

echo "Mirror complete. Verify with: oc image info ${dest_repo}:<tag>"
