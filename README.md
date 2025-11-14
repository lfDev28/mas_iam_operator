# MAS IAM Dev Stack – User Setup Guide

Hey team! I built this repo so we have a quick way to spin up a
SAML + LDAP resources on OpenShift for SAML, SCIM, or LDAP
testing. One manifest installs everything, the pods are already wired together, SSL is configured
as well as demo data for testing, so you can jump straight to connecting MAS without
worrying about provisioning all of the resources and dealing with all of the challenges that come 
with that.

Use this guide to install the stack, grab the Keycloak admin secret, and follow
the attached IBM documentation to configure SAML using Keycloak. OpenLDAP is
already federated into Keycloak so the demo users appear immediately, but the
raw LDAP endpoint and bind credentials are also available if you want MAS to
talk straight to LDAP for SCIM or custom tests.

## Prerequisites

- Access to an OpenShift cluster with cluster-admin or a project where you can
  create namespaces, Operators, and pods.
- The `oc` CLI. On macOS: `brew install openshift-cli`. Other platforms:
  <https://docs.openshift.com/container-platform/4.15/cli_reference/openshift_cli/getting-started-cli.html>
- A kubeadmin or personal API token for `oc login`.
- Optional: trust the OpenShift ingress CA if you plan to access the Keycloak
  route from outside the cluster (see “Ingress certificates” below).

## 1. Log in with `oc`

```bash
oc login --server https://api.<cluster-domain>:6443 --token <your-token>
```

Confirm you can query the cluster:

```bash
oc whoami
oc projects
```

## 2. Apply the consolidated manifest

The manifest installs the OLM catalog source, OperatorGroup, Subscription, TLS
bootstrap job, and a sample `MasIamStack` in the `iam` namespace. Create the
namespace first if it does not already exist:

```bash
oc new-project iam
# or
oc create namespace iam
```

```bash
oc apply -f https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/manifests/install-olm.yaml
```

Watch the namespace until every pod is running or completed:

```bash
oc get pods -n iam
```

You should see the Operator, Keycloak, OpenLDAP, PostgreSQL, and the two jobs
(`mas-iam-sample-generate-openldap-tls`, `mas-iam-sample-ldap-config`).

## 3. Retrieve the Keycloak admin credentials

The chart creates `<release>-bootstrap-admin` (defaults to
`mas-iam-sample-bootstrap-admin`) with a random password. Export it with:

```bash
oc get secret mas-iam-sample-bootstrap-admin \
  -n iam -o jsonpath='{.data.username}' | base64 -d && echo
oc get secret mas-iam-sample-bootstrap-admin \
  -n iam -o jsonpath='{.data.password}' | base64 -d && echo
```

## 4. Locate the Keycloak route

The install manifest exposes Keycloak via an OpenShift Route. Determine the host
with:

```bash
oc get route mas-iam-sample -n iam -o jsonpath='{.spec.host}{"\n"}'
```

Open `https://<route-host>` in your browser and sign in with the bootstrap admin
credentials from the previous step.

## 5. Understand what’s pre-provisioned

- Keycloak 26 with a sample realm ready for MAS integration.
- OpenLDAP seeded with demo users listed in the `MasIamStack` spec and already
  wired into Keycloak’s user federation.
- Bitnami PostgreSQL backing Keycloak.
- TLS secret generation handled automatically by the Job (retrieve the
  truststore password with:
  `oc get secret mas-iam-sample-keycloak-openldap-tls -n iam -o jsonpath='{.data.truststorePassword}' | base64 -d && echo`).
- Keycloak/OpenLDAP service accounts already bound to the `anyuid` SCC—no extra
  `oc adm policy` commands are required.

## 6. Import or configure MAS clients

Follow IBM’s MAS SAML client instructions and adapt them to this Keycloak realm:
<https://www.ibm.com/support/pages/examples-set-mas-saml-different-idps>.

Tips:

1. Use the MAS admin UI (Manage → Administration → Security) to register a new
   SAML client that points to your MAS workspace URL.
2. In Keycloak, either import the MAS-provided client descriptor or create a new
   client with the redirect URIs and certificates from the IBM guide.
3. When mapping attributes, reuse the LDAP-backed test users (e.g.,
   `alex.manager`, `jane.doe`) or create your own inside Keycloak.

## 7. SCIM API (preview)

The repo ships a Keycloak image (see `images/keycloak-scim/`) that includes the
Metatavu SCIM extension. To enable it:

1. **Build & publish the Keycloak image with the SCIM provider**  
   ```bash
   SCIM_KEYCLOAK_IMG=quay.io/<org>/mas-iam-keycloak:scim-0.0.1 make scim-keycloak-push
   ```

2. **Update the `MasIamStack`** so Keycloak uses that image and turns on SCIM:
   ```yaml
   spec:
     keycloak:
       image:
         registry: quay.io
         repository: <org>/mas-iam-keycloak
         tag: scim-0.0.1
       scim:
         enabled: true
         authenticationMode: KEYCLOAK
   ```

3. **Seed the SCIM roles and client** once the Keycloak pod is running:
   ```bash
   ./scripts/configure-scim-client.sh \
     --namespace iam \
     --release mas-iam-sample \
     --client-id scim-admin \
     --client-secret <strong-secret>
   ```
   The script logs into the Keycloak admin API, creates the `scim-access` /
   `scim-managed` roles, and configures a confidential client with service
   accounts enabled. Store the generated client credentials in a Kubernetes
   Secret so MAS (or other tools) can fetch them safely.

4. **Call the SCIM endpoint** using the service account’s token (client credentials flow):

```bash
KC_ROUTE=$(oc get route mas-iam-sample -n iam -o jsonpath='{.spec.host}')
SCIM_SECRET=<same secret passed to the script>
TOKEN=$(curl -sk -d grant_type=client_credentials \
  -d client_id=scim-admin \
  -d client_secret="${SCIM_SECRET}" \
  https://"${KC_ROUTE}"/realms/master/protocol/openid-connect/token | jq -r .access_token)

curl -sk -H "Authorization: Bearer ${TOKEN}" \
  https://"${KC_ROUTE}"/realms/master/scim/v2/Users?count=5 | jq .
```

[Switch to `EXTERNAL` mode](https://github.com/Metatavu/keycloak-scim-server)
by editing `spec.keycloak.scim` in the `MasIamStack` (issuer, audience, and JWKS
URI). Refer to the
[extension README](https://github.com/Metatavu/keycloak-scim-server) for Azure
Entra configuration details—the chart surfaces the required environment
variables so you can match their instructions. Future work will automate the
client/role bootstrap inside the chart; for now, the helper script keeps the
steps consistent.

## 8. Working directly with LDAP

If you need to run `ldapsearch` or modify the directory, exec into the OpenLDAP
pod:

```bash
oc rsh deployment/mas-iam-sample-openldap
# inside the pod
ldapsearch -x -H ldaps://mas-iam-sample-openldap:636 -D "cn=admin,dc=demo,dc=local" -W
```

The admin password lives in the `mas-iam-sample-openldap-admin` secret. Use
`oc get secret ... -o jsonpath='{.data.password}' | base64 -d` to retrieve it.
Individual demo user passwords live in
`mas-iam-sample-openldap-user-passwords` (keys match each username). Example:

```bash
oc get secret mas-iam-sample-openldap-user-passwords \
  -n iam -o jsonpath='{.data.alex\.manager}' | base64 -d && echo
```

### Direct MAS-to-LDAP wiring (optional)

If you want MAS (or another client) to authenticate directly against LDAP:

- **Host / port:** `mas-iam-sample-openldap.iam.svc.cluster.local:636`
- **Bind DN:** `cn=admin,dc=demo,dc=local`
- **Bind password:** `oc get secret mas-iam-sample-openldap-admin -n iam -o jsonpath='{.data.password}' | base64 -d`
- **TLS truststore / CA:** `mas-iam-sample-keycloak-openldap-tls` (contains
  `ca.crt`, `tls.crt`, `tls.key`, `ldap-truststore.p12`)

Import `ca.crt` wherever MAS (or your browser) needs to trust the OpenLDAP
endpoint. The same credentials will apply when the SCIM service becomes
available.

## 9. Resetting the namespace

If you need to wipe the environment, run the helper script **from the repo
root**:

```bash
curl -sS https://raw.githubusercontent.com/lfDev28/mas_iam_operator/main/scripts/reset-namespace.sh -o reset-namespace.sh
chmod +x reset-namespace.sh
./reset-namespace.sh --namespace iam --force
```

It removes the `MasIamStack`, job artifacts, PVCs, and operator subscription but
leaves the TLS material unless you delete it separately. After the cleanup,
reapply the install manifest from step 2.

## 10. Ingress certificates

OpenShift routes are signed by the cluster’s default ingress CA. If your browser
doesn’t trust it, download the CA bundle and import it:

```bash
oc get configmap -n openshift-config-managed default-ingress-cert -o jsonpath='{.data.ca-bundle\.crt}' > ingress-ca.crt
```

Add `ingress-ca.crt` to your operating system or browser trust store so the
Keycloak route shows as secure.

## 11. Future enhancements

- **SCIM server integration:** a SCIM-compatible provisioning service is in
  development so MAS can provision/deprovision users automatically.
- Additional documentation will cover the SCIM endpoint, required credentials,
  and how to flip MAS into SCIM mode once the server is available.

## Troubleshooting & Tips

- Rerun the install manifest (`oc apply -f …`) after upgrades; it is idempotent.
- Keep the bundled pods running by deleting failed jobs/pods before reapplying.
- If `oc apply -f …` shows “resource … configured”, that’s expected—it’s
  idempotent.
- The Operator and bundle images referenced in the manifest live on Quay. If you
  mirror them, update the `IMG`/`BUNDLE_IMG`/`CATALOG_IMG` variables before
  running `make docker-build docker-push`.

If you have any question please let me know.
