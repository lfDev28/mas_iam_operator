#!/usr/bin/env bash
# Container test for the chart's OpenLDAP storage init script
# (operators/mas-iam-operator/helm-charts/mas-iam-stack/openldap-scripts/storage-init.sh).
#
#   scripts/test-openldap-storage.sh          # or: make test-openldap-storage
#
# Reproduces what an upgraded cluster sees: a volume written by chart <= 0.1.7
# (database at the volume root, cn=config on an emptyDir), with an entry added
# after install. Then runs the init script and starts OpenLDAP the way the
# current chart does (data/ and config/ subPaths), and asserts:
#   1. the legacy volume crash-loops the way the cluster did (sanity)
#   2. the migration keeps the database (the post-install entry survives)
#   3. the migrated directory restarts with no init work (idempotent)
#   4. an empty volume bootstraps fresh
# Needs podman (or docker with CONTAINER_ENGINE=docker) and network access to
# pull the OpenLDAP image. Uses throwaway named volumes; touches no cluster.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART="${ROOT_DIR}/operators/mas-iam-operator/helm-charts/mas-iam-stack"
SCRIPT="${STORAGE_INIT_SCRIPT:-${CHART}/openldap-scripts/storage-init.sh}"
ENGINE="${CONTAINER_ENGINE:-podman}"
IMG="${OPENLDAP_IMAGE:-$(sed -n '/^openldap:/,/^[a-z]/{/repository:/{s/.*repository: *//p;};}' "${CHART}/values.yaml" | head -1):$(sed -n '/^openldap:/,/^[a-z]/{/tag:/{s/.*tag: *"\{0,1\}\([^"]*\)"\{0,1\}/\1/p;};}' "${CHART}/values.yaml" | head -1)}"
PLATFORM="${PLATFORM:-linux/amd64}"
ID="mas-est-ldaptest-$$"
BASE_DN="dc=demo,dc=local"
PW="test-admin-pw"
ENV=(-e LDAP_ORGANISATION=Demo -e LDAP_DOMAIN=demo.local -e "LDAP_BASE_DN=${BASE_DN}" -e "LDAP_ADMIN_PASSWORD=${PW}" -e LDAP_TLS=false)

failures=0
pass() { echo "PASS  $*"; }
bad()  { echo "FAIL  $*"; failures=$((failures + 1)); }
eng()  { "${ENGINE}" "$@"; }

cleanup() {
  eng rm -f "${ID}-run" >/dev/null 2>&1 || true
  eng volume rm -f "${ID}-legacy" "${ID}-empty" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# wait_ldap <container>: until an anonymous-bind search of the base succeeds.
# Time-based: under amd64 emulation each exec can take seconds.
wait_ldap() {
  local deadline=$((SECONDS + ${LDAP_WAIT_SECONDS:-180}))
  while (( SECONDS < deadline )); do
    if eng exec "$1" ldapsearch -x -H ldap://localhost:389 -D "cn=admin,${BASE_DN}" -w "${PW}" \
        -b "${BASE_DN}" -s base dn >/dev/null 2>&1; then return 0; fi
    if [[ "$(eng inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" != true ]]; then return 1; fi
    sleep 2
  done
  echo "      (gave up waiting for $1 after ${LDAP_WAIT_SECONDS:-180}s)"
  return 1
}

# start_new_layout <volume>: OpenLDAP as the current chart mounts it.
start_new_layout() {
  eng rm -f "${ID}-run" >/dev/null 2>&1 || true
  eng run -d --platform "${PLATFORM}" --name "${ID}-run" "${ENV[@]}" \
    --mount "type=volume,src=$1,dst=/var/lib/ldap,volume-subpath=data" \
    --mount "type=volume,src=$1,dst=/etc/ldap/slapd.d,volume-subpath=config" \
    "${IMG}" >/dev/null
}

run_init() { # run_init <volume> -> prints the script's output
  eng run --rm --platform "${PLATFORM}" "${ENV[@]}" -v "$1:/ldap-volume" \
    -v "${SCRIPT}:/storage-init.sh:ro" --entrypoint /bin/bash "${IMG}" /storage-init.sh 2>&1
}

echo "image: ${IMG} (${PLATFORM})"
eng pull -q --platform "${PLATFORM}" "${IMG}" >/dev/null

# --- 1. legacy volume: chart <= 0.1.7 layout -------------------------------
eng volume create "${ID}-legacy" >/dev/null
eng run -d --platform "${PLATFORM}" --name "${ID}-run" "${ENV[@]}" -v "${ID}-legacy:/var/lib/ldap" "${IMG}" >/dev/null
wait_ldap "${ID}-run" || { eng logs "${ID}-run" | tail -20; echo "error: legacy OpenLDAP never started" >&2; exit 1; }
eng exec -i "${ID}-run" ldapadd -x -H ldap://localhost:389 -D "cn=admin,${BASE_DN}" -w "${PW}" >/dev/null <<LDIF
dn: uid=added.after.install,${BASE_DN}
objectClass: inetOrgPerson
uid: added.after.install
cn: Added After Install
sn: Install
LDIF
# Graceful stop (SIGTERM, as a pod deletion does) so slapd flushes the write.
eng stop -t 30 "${ID}-run" >/dev/null
eng rm -f "${ID}-run" >/dev/null
# Same volume, fresh (emptyDir) config: what every pod restart looked like.
eng run -d --platform "${PLATFORM}" --name "${ID}-run" "${ENV[@]}" -v "${ID}-legacy:/var/lib/ldap" "${IMG}" >/dev/null
if wait_ldap "${ID}-run"; then
  bad "legacy layout restarted cleanly; the test no longer reproduces the crash loop"
elif grep -q 'config directory (/etc/ldap/slapd.d) is empty but not the database directory' <<<"$(eng logs "${ID}-run" 2>&1)"; then
  pass "legacy layout reproduces the crash loop ('config directory ... is empty')"
else
  bad "legacy restart failed for an unexpected reason:"; eng logs "${ID}-run" 2>&1 | tail -5 | sed 's/^/      /'
fi
eng rm -f "${ID}-run" >/dev/null

# --- 2. migrate and start with the new layout ------------------------------
out="$(run_init "${ID}-legacy")"
if grep -q 'data kept' <<<"${out}"; then pass "init script: rebuilt cn=config, data kept"; else
  bad "init script did not keep the data:"; tail -15 <<<"${out}" | sed 's/^/      /'
fi
start_new_layout "${ID}-legacy"
if wait_ldap "${ID}-run"; then
  pass "OpenLDAP starts on the migrated volume"
  # Capture, then grep: grep -q exiting early SIGPIPEs the writer, which
  # pipefail would report as a miss.
  found="$(eng exec "${ID}-run" ldapsearch -x -H ldap://localhost:389 -D "cn=admin,${BASE_DN}" -w "${PW}" \
      -b "${BASE_DN}" '(uid=added.after.install)' dn 2>&1 || true)"
  if grep -q '^dn: uid=added.after.install' <<<"${found}"; then
    pass "entry added after install survived the migration"
  else
    bad "entry added after install is missing after the migration:"; sed 's/^/      /' <<<"${found}"
  fi
  if eng exec "${ID}-run" ldapwhoami -x -H ldap://localhost:389 -D "cn=admin,${BASE_DN}" -w "${PW}" >/dev/null 2>&1; then
    pass "admin bind works with the rebuilt config"
  else
    bad "admin bind fails with the rebuilt config"
  fi
else
  bad "OpenLDAP does not start on the migrated volume:"; eng logs "${ID}-run" 2>&1 | tail -10 | sed 's/^/      /'
fi
eng rm -f "${ID}-run" >/dev/null

# --- 3. idempotent re-run ---------------------------------------------------
out="$(run_init "${ID}-legacy")"
if grep -q 'nothing to do' <<<"${out}"; then pass "second init run is a no-op"; else
  bad "second init run did work:"; tail -8 <<<"${out}" | sed 's/^/      /'
fi

# --- 4. fresh volume --------------------------------------------------------
eng volume create "${ID}-empty" >/dev/null
out="$(run_init "${ID}-empty")"
grep -q 'Empty volume' <<<"${out}" && pass "empty volume: init leaves bootstrap to OpenLDAP" || bad "empty volume: unexpected init output"
start_new_layout "${ID}-empty"
if wait_ldap "${ID}-run"; then pass "fresh volume bootstraps"; else
  bad "fresh volume does not bootstrap:"; eng logs "${ID}-run" 2>&1 | tail -10 | sed 's/^/      /'
fi

echo
if [[ "${failures}" -eq 0 ]]; then echo "openldap storage: GREEN"; exit 0; fi
echo "openldap storage: RED, ${failures} failure(s)"; exit 1
