# MAS IAM Dev Stack – User Setup Guide


This is the single copy/paste guide for installing all components on a cluster:

1. MAS IAM operator (OLM)
2. Sample MAS IAM stack (Keycloak + OpenLDAP + PostgreSQL)
3. SCIM bridge (Keycloak -> MAS)

All required placeholders are called out below.

## 0) Prerequisites

- `oc` CLI installed and logged into target cluster.
- Namespace `iam` available (or choose another namespace and replace it in commands/manifests).
- MAS API key (name + value) with SCIM permissions.
- MAS workspace ID (for demo profile bootstrap).
- A default StorageClass set in the cluster (used for the SCIM bridge PVC). Verify with:

```bash
oc get sc
oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.storageclass\\.kubernetes\\.io/is-default-class}{"\n"}{end}'
```

## 1) Cluster login and namespace

```bash
oc login --server https://api.<cluster-domain>:6443 --token <your-token>
oc whoami
oc new-project iam || oc project iam
```

## 2) Install operator components

Apply operator-only resources:

```bash
oc apply -f https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/manifests/install-olm.yaml
```

Wait for CSV to succeed:

```bash
oc get csv -n iam -w
```

## 3) Install sample MAS IAM stack (dev defaults)

Apply sample resources (dev TLS job + sample `MasIamStack` + demo secrets):

```bash
oc apply -f https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/manifests/install-olm-sample.yaml
```

Watch pods/jobs:

```bash
oc get pods -n iam -w
```

Notes:
- Sample manifest creates demo passwords (`maxadmin`) for Keycloak/OpenLDAP/PostgreSQL.
- For production-like installs, replace sample secrets with your own before applying a custom `MasIamStack`.

## 4) Verify IAM stack and fetch useful values

Keycloak route:

```bash
oc get route mas-iam-sample -n iam -o jsonpath='{.spec.host}{"\n"}'
```

Note:
- This stack does not pre-create MAS application clients in Keycloak. MAS must register its own client(s) in the `maximo` realm as part of the MAS authentication/IdP setup flow.
- If you previously integrated MAS with this Keycloak and then you reset the IAM stack by deleting the PostgreSQL PVC, you wiped the Keycloak database and those MAS client registrations will be lost. In that case, rerun the MAS authentication/IdP registration step (or restore the old database).

Keycloak bootstrap admin credentials:

```bash
oc get secret mas-iam-sample-bootstrap-admin -n iam -o jsonpath='{.data.username}' | base64 -d; echo
oc get secret mas-iam-sample-bootstrap-admin -n iam -o jsonpath='{.data.password}' | base64 -d; echo
```

OpenLDAP details (for external registry setup):

```text
LDAP URL:  ldaps://mas-iam-sample-openldap.iam.svc.cluster.local:636
Base DN:   dc=demo,dc=local
Users DN:  ou=users,dc=demo,dc=local
Bind DN:   cn=admin,dc=demo,dc=local
```

OpenLDAP bind password:

```bash
oc get secret mas-iam-sample-openldap-admin -n iam -o jsonpath='{.data.password}' | base64 -d; echo
```

## 5) Install SCIM bridge (required values only)

Set required install values:

```bash
export SCIM_BRIDGE_MAS_BASE_URL='https://api.<mas-instance>.<domain>/scim/v2'
export SCIM_BRIDGE_MAS_API_TOKEN_NAME='<your-mas-api-key-name>'
export SCIM_BRIDGE_MAS_API_TOKEN_VALUE='<your-mas-api-key-value>'
export SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID='<workspace-id>'
```

Important:
- `SCIM_BRIDGE_MAS_BASE_URL` must include `/scim/v2` (do not use just the MAS API root).
  Example: `https://api.lfmas.apps.<domain>/scim/v2`

Install with template:

```bash
oc process -f https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/manifests/scim-bridge-install-template.yaml \
  -p SCIM_BRIDGE_MAS_BASE_URL="${SCIM_BRIDGE_MAS_BASE_URL}" \
  -p SCIM_BRIDGE_MAS_API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
  -p SCIM_BRIDGE_MAS_API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
  -p SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID="${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID}" \
| oc apply -f -
```

## 6) Optional SCIM bridge overrides

Pass any of these with extra `-p` flags if needed:

```text
SCIM_BRIDGE_NAMESPACE=iam
SCIM_BRIDGE_IMAGE=quay.io/<org>/mas-iam-scim-bridge:<tag>
SCIM_BRIDGE_KEYCLOAK_IMAGE=quay.io/<org>/mas-iam-keycloak:<tag>
SCIM_BRIDGE_KEYCLOAK_BASE_URL=http://mas-iam-sample:8080
SCIM_BRIDGE_KEYCLOAK_REALM=maximo
SCIM_BRIDGE_KEYCLOAK_CLIENT_ID=scim-admin
SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET=maxadmin
SCIM_BRIDGE_MAS_PROFILE_ID=demo
SCIM_BRIDGE_BRIDGE_POLL_INTERVAL=5m
SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY=false
SCIM_BRIDGE_MAS_CA_FILE=/etc/scim-bridge/certs/mas-ca.crt
```

If MAS uses a custom route CA, include:

```bash
-p SCIM_BRIDGE_MAS_CA_BUNDLE="$(oc get route <mas-api-route> -n <mas-core-namespace> -o jsonpath='{.spec.tls.caCertificate}')" \
-p SCIM_BRIDGE_MAS_CA_FILE=/etc/scim-bridge/certs/mas-ca.crt \
```

## 7) Verify SCIM bridge install

Check workload status:

```bash
oc get pods -n iam | grep scim-bridge
oc get jobs -n iam | grep scim-bridge
oc get pvc -n iam | grep scim-bridge
```

Expected jobs:
- `scim-bridge-keycloak-bootstrap` -> `Complete`
- `scim-bridge-mas-profile-bootstrap` -> `Complete` (when enabled)

Copy/paste verification:

```bash
oc wait --for=condition=complete job/scim-bridge-keycloak-bootstrap -n iam --timeout=10m
oc wait --for=condition=complete job/scim-bridge-mas-profile-bootstrap -n iam --timeout=10m || true
oc rollout status deploy/scim-bridge -n iam --timeout=5m
oc logs deploy/scim-bridge -n iam --since=10m --tail=200
```

Check bridge logs:

```bash
oc logs deploy/scim-bridge -n iam --tail=200
```

## 8) Day-2 operations

Restart bridge after ConfigMap/Secret edits:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

Rotate MAS API key (recommended method):

```bash
oc create secret generic scim-bridge-secret -n iam \\
  --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_NAME='<your-mas-api-key-name>' \\
  --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_VALUE='<your-mas-api-key-value>' \\
  --dry-run=client -o yaml \\
| oc apply -f -

oc rollout restart deployment/scim-bridge -n iam
```

If the bridge is skipping users due to a prior `401` or other sticky error, run the retry tool:
1. Open terminal in the `scim-bridge` pod, container `state-tools`.
2. Run `/opt/scim-bridge-tools/retry-errors --all-errors` and then restart the Deployment.

If Keycloak client secret changed, rerun Keycloak bootstrap job:

```bash
oc delete job scim-bridge-keycloak-bootstrap -n iam --ignore-not-found
oc process -f https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/manifests/scim-bridge-install-template.yaml \
  -p SCIM_BRIDGE_MAS_BASE_URL="${SCIM_BRIDGE_MAS_BASE_URL}" \
  -p SCIM_BRIDGE_MAS_API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
  -p SCIM_BRIDGE_MAS_API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
  -p SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID="${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID}" \
| oc apply -f -
```

If MAS profile bootstrap values changed, rerun profile bootstrap job:

```bash
oc delete job scim-bridge-mas-profile-bootstrap -n iam --ignore-not-found
oc process -f https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/manifests/scim-bridge-install-template.yaml \
  -p SCIM_BRIDGE_MAS_BASE_URL="${SCIM_BRIDGE_MAS_BASE_URL}" \
  -p SCIM_BRIDGE_MAS_API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
  -p SCIM_BRIDGE_MAS_API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
  -p SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID="${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID}" \
| oc apply -f -
```

## 9) Recover from sticky SCIM errors (without deleting PVC)

If logs show `skip update due to prior MAS error`, clear sticky error entries via the `state-tools` container.

1. Open terminal in pod `scim-bridge-...`, container `state-tools`.
2. Run:

```bash
/opt/scim-bridge-tools/retry-errors --list
/opt/scim-bridge-tools/retry-errors --all-errors
oc rollout restart deployment/scim-bridge -n iam
```

Target a single user:

```bash
/opt/scim-bridge-tools/retry-errors --username jane.doe
oc rollout restart deployment/scim-bridge -n iam
```

## 10) Quick uninstall/reset (dev)

```bash
curl -sS https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/scripts/reset-namespace.sh -o reset-namespace.sh
chmod +x reset-namespace.sh
./reset-namespace.sh --namespace iam --force
```

Add `--purge-tls` to also remove LDAP TLS material.
The reset flow scales PostgreSQL down and removes the Postgres PVC so fresh installs
do not inherit stale database credentials.

## 11) Minimal handoff checklist for colleagues

- Operator CSV is `Succeeded` in namespace `iam`.
- Keycloak/OpenLDAP/PostgreSQL pods are running.
- `scim-bridge` pod is running.
- SCIM bootstrap jobs are completed.
- `scim-bridge-secret` contains environment-specific MAS API key values.
- Keycloak route is reachable.
