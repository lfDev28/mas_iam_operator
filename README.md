# MAS IAM Dev Stack – User Setup Guide

This repository publishes a development-friendly Keycloak + OpenLDAP + PostgreSQL
stack that mirrors the MAS IAM topology. The consolidated install manifest wires
everything up on OpenShift so you can start testing MAS integrations quickly.

Use this guide to install the stack, retrieve the Keycloak admin credentials,
and connect MAS following IBM’s documented SAML examples.

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
bootstrap job, and a sample `MasIamStack` in the `iam` namespace.

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
- OpenLDAP seeded with demo users listed in the `MasIamStack` spec.
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

## 7. Working directly with LDAP

If you need to run `ldapsearch` or modify the directory, exec into the OpenLDAP
pod:

```bash
oc rsh deployment/mas-iam-sample-openldap
# inside the pod
ldapsearch -x -H ldaps://mas-iam-sample-openldap:636 -D "cn=admin,dc=demo,dc=local" -W
```

The admin password lives in the `mas-iam-sample-openldap-admin` secret. Use
`oc get secret ... -o jsonpath='{.data.password}' | base64 -d` to retrieve it.

## 8. Ingress certificates

OpenShift routes are signed by the cluster’s default ingress CA. If your browser
doesn’t trust it, download the CA bundle and import it:

```bash
oc get configmap -n openshift-config-managed default-ingress-cert -o jsonpath='{.data.ca-bundle\.crt}' > ingress-ca.crt
```

Add `ingress-ca.crt` to your operating system or browser trust store so the
Keycloak route shows as secure.

## 9. Future enhancements

- **SCIM server integration:** a SCIM-compatible provisioning service is in
  development so MAS can provision/deprovision users automatically.
- Additional documentation will cover the SCIM endpoint, required credentials,
  and how to flip MAS into SCIM mode once the server is available.

## Troubleshooting & Tips

- Rerun the install manifest (`oc apply -f …`) after upgrades; it is idempotent.
- To reset the environment, use `./scripts/reset-namespace.sh --namespace iam --force`,
  then reapply the manifest.
- The Operator and bundle images referenced in the manifest live on Quay. If you
  mirror them, update the `IMG`/`BUNDLE_IMG`/`CATALOG_IMG` variables before
  running `make docker-build docker-push`.

Have questions or hit an issue? File it in this repo and include the relevant
pod logs (`oc logs -n iam <pod>`).
