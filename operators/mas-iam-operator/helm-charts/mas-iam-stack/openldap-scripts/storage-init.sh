# openldap-storage init container (see templates/openldap-deployment.yaml).
#
# Volume layout (chart >= 0.1.8):
#   <volume>/data      -> /var/lib/ldap      (mdb database)
#   <volume>/config    -> /etc/ldap/slapd.d  (cn=config)
#   <volume>/backup-*  -> anything this script moved aside
#
# osixia/openldap 1.5.0 startup.sh only starts when the database and config
# directories are both empty (fresh bootstrap + seed) or both populated. This
# script brings the volume into one of those two states:
#   1. Legacy layout (DB files at the volume root): move them into data/.
#   2. DB present, config missing (the old emptyDir config): rebuild cn=config
#      with a throwaway bootstrap of the same image and settings, then check it
#      opens the existing DB with slapcat. Data is kept.
#   3. If that check fails: move the DB to backup-<ts>/data so OpenLDAP
#      bootstraps and re-seeds from the chart LDIF. Entries added after
#      install are no longer in the live directory (they stay in the backup).
#   4. Config present, DB missing: move the config aside; fresh bootstrap.
set -uo pipefail
shopt -s nullglob

VOL=/ldap-volume
DATA="${VOL}/data"
CONF="${VOL}/config"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP="${VOL}/backup-${TS}"

log() { echo "[openldap-storage] $*"; }
loud() {
  echo "[openldap-storage] ######################################################################" >&2
  for line in "$@"; do
    echo "[openldap-storage] !!! ${line}" >&2
  done
  echo "[openldap-storage] ######################################################################" >&2
}
# Same emptiness test osixia's startup.sh uses.
populated() { [[ -n "$(ls -A -I lost+found --ignore='.*' "$1" 2>/dev/null)" ]]; }

mkdir -p "${DATA}" "${CONF}" || { log "cannot create ${DATA} / ${CONF}"; exit 1; }

# 1. Legacy layout: chart <= 0.1.7 mounted the volume root at /var/lib/ldap.
legacy=()
for entry in "${VOL}"/*; do
  case "$(basename "${entry}")" in
    data|config|backup-*|lost+found) continue ;;
  esac
  legacy+=("${entry}")
done
if (( ${#legacy[@]} > 0 )); then
  if populated "${DATA}"; then
    loud "Found files at the volume root AND a populated data/ directory." \
      "Using data/; moving the root files to ${BACKUP}/volume-root for inspection."
    mkdir -p "${BACKUP}/volume-root" && mv "${legacy[@]}" "${BACKUP}/volume-root/" || exit 1
  else
    log "Legacy layout detected; moving ${legacy[*]} into data/."
    mv "${legacy[@]}" "${DATA}/" || exit 1
  fi
fi

if populated "${DATA}" && populated "${CONF}"; then
  log "Database and cn=config are both on the volume; nothing to do."
  exit 0
fi
if ! populated "${DATA}" && ! populated "${CONF}"; then
  log "Empty volume; OpenLDAP will bootstrap and seed on first start."
  exit 0
fi
if ! populated "${DATA}"; then
  loud "cn=config is on the volume but the database is not; osixia refuses to start like that." \
    "Moving config/ to ${BACKUP}/config so OpenLDAP bootstraps a fresh directory."
  mkdir -p "${BACKUP}" && mv "${CONF}" "${BACKUP}/config" && mkdir -p "${CONF}" || exit 1
  exit 0
fi

# 2. Database present, cn=config missing.
log "Database found without a persisted cn=config (chart <= 0.1.7 kept slapd.d on an emptyDir)."
log "Rebuilding cn=config with a throwaway bootstrap of this image (no TLS, no seed data)..."
rm -rf /etc/ldap/slapd.d /var/lib/ldap
mkdir -p /etc/ldap/slapd.d /var/lib/ldap
regenerated=false
# -k: do not SIGTERM every process on exit; --run-only startup: run startup.sh
# (bootstrap) and skip slapd's long-running process.
if /container/tool/run -k --run-only startup --loglevel info true; then
  rm -rf /var/lib/ldap
  ln -s "${DATA}" /var/lib/ldap
  # mdb does not record its suffix, so "slapcat works" is not enough: the
  # database must also hold the base entry for LDAP_BASE_DN, or the rebuilt
  # config (suffix, rootDN, ACLs) would not match the data.
  # ldif-wrap, with a hyphen: slapcat 2.4 rejects ldif_wrap with a usage error.
  if slapcat -F /etc/ldap/slapd.d -b "${LDAP_BASE_DN}" -o ldif-wrap=no -l /tmp/existing.ldif \
    && grep -qixF "dn: ${LDAP_BASE_DN}" /tmp/existing.ldif; then
    regenerated=true
  else
    log "The existing database does not open with the rebuilt config, or has no '${LDAP_BASE_DN}' entry."
  fi
  rm -f /var/lib/ldap
else
  log "Throwaway bootstrap failed."
fi

if [[ "${regenerated}" == "true" ]]; then
  # Not cp -a: that would carry the init container's SELinux label onto the
  # volume, and a container with another MCS label could not read it.
  cp -dR --preserve=mode,ownership,timestamps /etc/ldap/slapd.d/. "${CONF}/" || exit 1
  mkdir -p "${BACKUP}" && cp /tmp/existing.ldif "${BACKUP}/pre-migration-export.ldif"
  log "Rebuilt cn=config opens the existing database ($(grep -c '^dn: ' /tmp/existing.ldif) entries); data kept."
  log "An LDIF export taken before the migration is at ${BACKUP}/pre-migration-export.ldif."
  exit 0
fi

# 3. Fallback: start over, keep the old database for manual recovery.
loud "Could not rebuild a cn=config that opens the existing OpenLDAP database." \
  "Moving the database to ${BACKUP}/data; OpenLDAP will bootstrap a fresh directory" \
  "and re-seed the demo users from the chart LDIF." \
  "ANY LDAP ENTRY OR PASSWORD CHANGE MADE AFTER INSTALL IS NOT IN THE LIVE DIRECTORY." \
  "It is still in ${BACKUP}/data (mdb files); recover it with slapcat against a matching config."
mkdir -p "${BACKUP}" && mv "${DATA}" "${BACKUP}/data" && mkdir -p "${DATA}" || exit 1
rm -rf "${CONF:?}"/* "${CONF}"/.[!.]* 2>/dev/null
exit 0
