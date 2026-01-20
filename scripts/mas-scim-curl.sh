#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

# MAS SCIM cURL helper
# Fill the env vars below before running. The script keeps things explicit so you
# can reuse the same values for the scim-bridge or Postman.
: "${MAS_SCIM_BASE:?Set MAS_SCIM_BASE (e.g., https://api.mas.example/scim/v2)}"
: "${MAS_PROFILE_ID:?Set MAS_PROFILE_ID (SCIM profile ID in MAS)}"
# If MAS_TOKEN is unset, the script will attempt to obtain one via basic auth
# against the MAS /v1/authenticate endpoint using API_TOKEN_NAME/API_TOKEN_VALUE.
MAS_TOKEN=${MAS_TOKEN:-}
API_TOKEN_NAME=${API_TOKEN_NAME:-}
API_TOKEN_VALUE=${API_TOKEN_VALUE:-}
# MAS_AUTH_SCHEME controls the Authorization header. Use APIKey for MAS API keys,
# or Bearer for JWTs. Example: MAS_AUTH_SCHEME=APIKey or MAS_AUTH_SCHEME=Bearer.
MAS_AUTH_SCHEME=${MAS_AUTH_SCHEME:-APIKey}

strip_trailing_scim() {
  local base="$1"
  case "$base" in
    */scim/v2)
      echo "${base%/scim/v2}"
      ;;
    *)
      echo "$base"
      ;;
  esac
}

MAS_API_ROOT=$(strip_trailing_scim "${MAS_SCIM_BASE%/}")
AUTH_URL="${MAS_API_ROOT%/}/v1/authenticate"
BASE="${MAS_SCIM_BASE%/}/${MAS_PROFILE_ID}"
PROFILES_BASE="${MAS_SCIM_BASE%/}/Profiles"

obtain_token_if_needed() {
  if [[ -n "$MAS_TOKEN" ]]; then
    return
  fi
  if [[ -z "$API_TOKEN_NAME" || -z "$API_TOKEN_VALUE" ]]; then
    echo "error: MAS_TOKEN not set and API_TOKEN_NAME/API_TOKEN_VALUE not provided" >&2
    exit 1
  fi
  MAS_TOKEN=$(curl -sk -u "$API_TOKEN_NAME:$API_TOKEN_VALUE" "$AUTH_URL" | jq -r '.token')
  if [[ -z "$MAS_TOKEN" || "$MAS_TOKEN" == "null" ]]; then
    echo "error: failed to obtain MAS token from $AUTH_URL" >&2
    exit 1
  fi
}

obtain_token_if_needed
AUTH_HEADER="$MAS_AUTH_SCHEME $MAS_TOKEN"

list_users() {
  curl -sk \
    -H "Authorization: ${AUTH_HEADER}" \
    "${BASE}/Users?startIndex=1&count=20" | jq .
}

create_user() {
  local username=${1:-scim.user}
  local email=${2:-scim.user@example.com}
  curl -sk \
    -H "Authorization: ${AUTH_HEADER}" \
    -H "Content-Type: application/scim+json" \
    -d @- \
    "${BASE}/Users" <<EOF
{
  "schemas": ["urn:ietf:params:scim:schemas:core:2.0:User"],
  "userName": "${username}",
  "active": true,
  "displayName": "${username}",
  "name": {
    "givenName": "${username}",
    "familyName": "user"
  },
  "emails": [
    {
      "value": "${email}",
      "primary": true
    }
  ]
}
EOF
}

list_profiles() {
  curl -sk \
    -H "Authorization: ${AUTH_HEADER}" \
    "${PROFILES_BASE}" | jq .
}

get_profile() {
  local profile_id=${1:?profile ID required}
  curl -sk \
    -H "Authorization: ${AUTH_HEADER}" \
    "${PROFILES_BASE}/${profile_id}" | jq .
}

create_profile() {
  local profile_id=${1:?profile ID required}
  curl -sk \
    -H "Authorization: ${AUTH_HEADER}" \
    -H "Content-Type: application/scim+json" \
    -d @- \
    "${PROFILES_BASE}" <<EOF
{
  "id": "${profile_id}",
  "version": 1,
  "identities": [
    {
      "type": "local"
    }
  ],
  "entitlement": {
    "application": "BASE"
  },
  "workspaces": [
    {
      "id": "lfws",
      "applications": ["manage"]
    }
  ]
}
EOF
}

delete_profile() {
  local profile_id=${1:?profile ID required}
  curl -sk -X DELETE \
    -H "Authorization: ${AUTH_HEADER}" \
    "${PROFILES_BASE}/${profile_id}"
}

show_usage() {
  cat <<'EOF'
Usage: MAS_SCIM_BASE=... MAS_PROFILE_ID=... [MAS_TOKEN=...] [API_TOKEN_NAME=... API_TOKEN_VALUE=...] [MAS_AUTH_SCHEME=APIKey|Bearer] ./scripts/mas-scim-curl.sh <command>

Commands:
  list                      List users via GET /Users
  create <u> <e>            Create user with username <u> and email <e>
  profiles                  List SCIM profiles
  profile <id>              Get a specific SCIM profile
  profile-create <id>       Create a SCIM profile (template body in script)
  profile-delete <id>       Delete a SCIM profile

Examples:
  MAS_SCIM_BASE=https://api.mas.example/scim/v2 \
  MAS_PROFILE_ID=default \
  MAS_TOKEN="<api-key>" \
  MAS_AUTH_SCHEME=APIKey \
  ./scripts/mas-scim-curl.sh list

  MAS_SCIM_BASE=https://api.mas.example/scim/v2 \
  MAS_PROFILE_ID=default \
  MAS_TOKEN="<jwt>" \
  MAS_AUTH_SCHEME=Bearer \
  ./scripts/mas-scim-curl.sh create alex.manager alex.manager@example.com

  # List profiles
  MAS_SCIM_BASE=https://api.mas.example/scim/v2 \
  MAS_PROFILE_ID=default \
  MAS_TOKEN="<api-key>" \
  ./scripts/mas-scim-curl.sh profiles

  # Create a profile with id 'default'
  MAS_SCIM_BASE=https://api.mas.example/scim/v2 \
  MAS_PROFILE_ID=default \
  MAS_TOKEN="<api-key>" \
  ./scripts/mas-scim-curl.sh profile-create default

  # Obtain MAS_TOKEN via /v1/authenticate using basic auth
  MAS_SCIM_BASE=https://api.mas.example/scim/v2 \
  MAS_PROFILE_ID=default \
  API_TOKEN_NAME="masadmin" \
  API_TOKEN_VALUE="<password>" \
  MAS_AUTH_SCHEME=Bearer \
  ./scripts/mas-scim-curl.sh list

EOF
}

cmd=${1:-}
shift || true

case "$cmd" in
  list)
    list_users
    ;;
  create)
    create_user "$@"
    ;;
  profiles)
    list_profiles
    ;;
  profile)
    get_profile "$@"
    ;;
  profile-create)
    create_profile "$@"
    ;;
  profile-delete)
    delete_profile "$@"
    ;;
  *)
    show_usage
    ;;
esac
