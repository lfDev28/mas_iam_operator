---
name: mas-est-wipe
description: Full MAS-EST cluster wipe — covers `mas-est` namespace AND the MAS-side state (`mas-{instance}-core` IDPCfgs, Mongo `User` docs, Maximo db2 user rows, OLM CatalogSource) that `mas-est uninstall` does NOT touch. Use when the user asks to wipe / reset / clean the cluster before reinstalling, when validating a fresh-cluster install, or after a failed install that needs a clean re-test. Requires `oc login` against the target cluster.
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Write
---

# /mas-est-wipe — Full MAS-EST cluster wipe

`mas-est uninstall` only deletes the `mas-est` namespace. It does NOT touch:

- IDPCfg CRs in `mas-{instance}-core` (LDAP/OIDC/SAML)
- The `{instance}-selfreg` ConfigMap
- The MAS Suite CR's `spec.podTemplates` override (`entitymgr-idpcfg` 2Gi bump)
- The OLM `CatalogSource` (`mas-iam-operator` in `openshift-marketplace`)
- MongoDB `User` documents for the test users (`oidc.user1`, `sysadmin`, etc.)
- Maximo `MAXUSER` / `PERSON` / `PERSONANCESTOR` rows
- The cached IDP config in `coreidp` / `coreidp-login` / `coreapi` deployments (the UI keeps showing OIDC/SAML/LDAP login buttons until they restart)

Leftovers cause two classes of bug:

1. **Stale Mongo `User` docs** (e.g. `sysadmin` with `_local` identity from a previous install) make fresh self-reg fail with `CWIML4537E` — the user sees "username/password invalid" even though their LDAP bind succeeded.
2. **Stale `MAXIMO.PERSONANCESTOR` rows** without matching `PERSON` rows make new user-sync fail with `AIUI1101E: 500: Internal Server Error` because the unique constraint blocks the parent insert.

The full wipe sequence below handles all of it. Run in order — skipping the Suite-CR memory bump in step 2 will leave the IDPCfg finalizers stuck in OOMKill loops.

## Prerequisites

- `oc whoami` succeeds against the target MAS cluster
- The user has confirmed they want to wipe (this is destructive across `mas-est`, `mas-{instance}-core`, MongoDB, and Maximo db2)
- Know the MAS instance ID (default: `mas91`) and the demo user list

## Inputs

If the user provided arguments via `$ARGUMENTS`, parse them. Otherwise use defaults:

- `--instance <id>` — MAS instance ID (default `mas91`)
- `--full-olm-reset` — also delete the `CatalogSource` (simulates a brand-new cluster's OLM state — useful when validating beta hardening like the OLM resolver race guard)
- `--skip-mongo` — leave MongoDB User docs alone
- `--skip-db2` — leave Maximo db2 rows alone

If unsure, default to: instance=`mas91`, full-olm-reset=NO, skip-mongo=NO, skip-db2=NO.

## Sequence

### 1. `mas-est uninstall`

```bash
mas-est uninstall --namespace mas-est --skip-profile-delete -y
```

`--skip-profile-delete` is intentional — we don't want to delete the MAS SCIM profile, just the lab namespace. Wait for this to complete (~30s).

### 2. Bump `entitymgr-idpcfg` memory via Suite CR (REQUIRED before IDPCfg delete)

The IDPCfg finalizer playbook needs >512Mi when several IDPCfgs reconcile or finalize concurrently. The default 512Mi OOMKills and the IDPCfgs hang on `idpcfgs.config.mas.ibm.com/finalizer`. The Suite + ibm-mas operators revert any direct deployment patch within ~2 minutes, so the bump MUST go via the Suite CR `spec.podTemplates`:

```bash
oc -n mas-{instance}-core patch suite {instance} --type=merge -p='{
  "spec":{
    "podTemplates":[{
      "name":"entitymgr-idpcfg",
      "containers":[{
        "name":"manager",
        "resources":{
          "limits":{"cpu":"0.2","memory":"2Gi"},
          "requests":{"cpu":"0.01","memory":"64Mi"}
        }
      }]
    }]
  }
}'
```

Wait until the deployment actually reflects 2Gi (operator reconcile takes ~30-60s):

```bash
until [ "$(oc -n mas-{instance}-core get deploy {instance}-entitymgr-idpcfg -o jsonpath='{.spec.template.spec.containers[?(@.name=="manager")].resources.limits.memory}')" = "2Gi" ]; do sleep 8; done
oc -n mas-{instance}-core rollout status deploy/{instance}-entitymgr-idpcfg --timeout=180s
```

### 3. Delete IDPCfg CRs + wait for finalizer

```bash
oc -n mas-{instance}-core delete idpcfg --all --wait=false
until [ -z "$(oc -n mas-{instance}-core get idpcfg -o name 2>/dev/null)" ]; do sleep 15; done
```

The finalizer can take 1-3 minutes. If it stalls >5 min, the entitymgr-idpcfg is likely OOMKilling — verify step 2 actually took effect.

### 4. Clean up IDPCfg cred secrets + selfreg ConfigMap

```bash
oc -n mas-{instance}-core delete secret -l app.kubernetes.io/managed-by=mas-est-installer --ignore-not-found
oc -n mas-{instance}-core delete cm {instance}-selfreg --ignore-not-found
```

The `app.kubernetes.io/managed-by=mas-est-installer` label is what the installer puts on the user-supplied cred secrets it creates. The pre-existing `{instance}-usersupplied-bas-creds-system` doesn't have this label and is preserved.

### 5. Roll Suite CR `podTemplates` back to empty

So the next install's idpcfg memory bump has work to do (and proves the installer's bump still works):

```bash
oc -n mas-{instance}-core patch suite {instance} --type=json -p='[{"op":"remove","path":"/spec/podTemplates"}]'
```

### 6. Delete CatalogSource (only if `--full-olm-reset`)

When validating the OLM resolver race guard from beta.19+, you want OLM to start from zero with no cached catalog state:

```bash
oc -n openshift-marketplace delete catalogsource mas-iam-operator --ignore-not-found
oc -n openshift-marketplace wait --for=delete pod -l olm.catalogSource=mas-iam-operator --timeout=120s
```

Skip this for routine wipes — the existing CatalogSource is fine to keep.

### 7. Wipe MongoDB `User` docs (unless `--skip-mongo`)

The MAS Suite's `mas_{instance}_core` MongoDB stores `User` documents per IDP-authenticated user. These survive `mas-est uninstall`. Stale docs with `owner: scim_2.0` + `_local` identity (from a previous SCIM bridge install that picked the user up before a prefix filter was added) block fresh self-reg via OIDC/SAML/LDAP.

The Mongo CE deployment lives in the `mongoce` namespace. The replica set is `mas-mongo-ce` with members `mas-mongo-ce-{0,1,2}`. Each pod has TLS-only access; the CA is at `/var/lib/tls/ca/*.pem`. Use any pod (NOT necessarily primary — mongosh can route writes):

```bash
MUSER=$(oc -n mas-{instance}-core get secret mas-mongo-credentials -o jsonpath='{.data.username}' | base64 -d)
MPASS=$(oc -n mas-{instance}-core get secret mas-mongo-credentials -o jsonpath='{.data.password}' | base64 -d)
oc -n mongoce exec mas-mongo-ce-2 -c mongod -- bash -c "
mongosh --quiet -u '$MUSER' -p '$MPASS' \
  --tls --tlsCAFile /var/lib/tls/ca/*.pem \
  'mongodb://mas-mongo-ce-0.mas-mongo-ce-svc.mongoce.svc.cluster.local,mas-mongo-ce-1.mas-mongo-ce-svc.mongoce.svc.cluster.local,mas-mongo-ce-2.mas-mongo-ce-svc.mongoce.svc.cluster.local/admin?replicaSet=mas-mongo-ce' \
  --eval '
const u = db.getSiblingDB(\"mas_{instance}_core\").User;
const ids = [\"sysadmin\",\"jane.doe\",\"joe.bloggs\",\"alex.manager\",\"ldap.user1\",\"ldap.user2\",\"oidc.user1\",\"oidc.user2\",\"saml.user1\",\"saml.user2\",\"scim.user1\",\"scim.user2\"];
print(\"mongo deleted:\", u.deleteMany({_id:{\$in:ids}}).deletedCount);
'"
```

Confirm the connection-string hostnames use `.svc.cluster.local` — the shorter forms fail with TLS hostname-verification errors.

### 8. Wipe Maximo db2 user rows (unless `--skip-db2`)

The db2u pod is `c-mas-{instance}-workspace-manage-db2u-0` in the `db2u` namespace. Always run sql via `su - db2inst1` to get the right environment. Connect string is `BLUDB`. Test users have UPPERCASE PERSONID/USERID:

```bash
cat > /tmp/wipe.sh <<'SH'
#!/bin/bash
db2 connect to BLUDB > /dev/null
db2 "delete from MAXIMO.MAXUSER where USERID in ('SYSADMIN','JANE.DOE','JOE.BLOGGS','ALEX.MANAGER','LDAP.USER1','LDAP.USER2','OIDC.USER1','OIDC.USER2','SAML.USER1','SAML.USER2','SCIM.USER1','SCIM.USER2')"
db2 "delete from MAXIMO.GROUPUSER where USERID in ('SYSADMIN','JANE.DOE','JOE.BLOGGS','ALEX.MANAGER','LDAP.USER1','LDAP.USER2','OIDC.USER1','OIDC.USER2','SAML.USER1','SAML.USER2','SCIM.USER1','SCIM.USER2')"
db2 "delete from MAXIMO.PERSONANCESTOR where PERSONID in ('SYSADMIN','JANE.DOE','JOE.BLOGGS','ALEX.MANAGER','LDAP.USER1','LDAP.USER2','OIDC.USER1','OIDC.USER2','SAML.USER1','SAML.USER2','SCIM.USER1','SCIM.USER2')"
db2 "delete from MAXIMO.PERSON where PERSONID in ('SYSADMIN','JANE.DOE','JOE.BLOGGS','ALEX.MANAGER','LDAP.USER1','LDAP.USER2','OIDC.USER1','OIDC.USER2','SAML.USER1','SAML.USER2','SCIM.USER1','SCIM.USER2')"
db2 commit
SH
oc -n db2u cp /tmp/wipe.sh c-mas-{instance}-workspace-manage-db2u-0:/tmp/wipe.sh
oc -n db2u exec c-mas-{instance}-workspace-manage-db2u-0 -- bash -lc 'su - db2inst1 -c "bash /tmp/wipe.sh"'
```

Tables to clean: `MAXUSER`, `GROUPUSER`, `PERSONANCESTOR`, `PERSON`. `GROUPUSER` is the one that bit Lee on 2026-06-30 — leaving stale `GROUPUSER` rows from a prior install causes the next user-sync to surface as `BMXAA10249E - Your security privileges do not allow access to add new users to DEFLTREG security group`. The error message is misleading: the *symptom* is a privilege error, but the *cause* is the GROUPUSER unique-key conflict on (USERID, GROUPNAME). The first user to sync through happens to win the race; everyone else fails.

Delete in order MAXUSER → GROUPUSER → PERSONANCESTOR → PERSON. The reverse order works too because each table is independent for our test-user IDs, but if you ever extend this to real customer users you may hit FK ordering.

### 9. Restart MAS core auth deployments

The login page providers list is rendered from the current IDPCfg state, BUT `coreidp` / `coreidp-login` / `coreapi` cache the Liberty user-registry config and the discovered IDP set in memory. Without a restart the MAS Admin UI keeps showing OIDC/SAML/LDAP login buttons even after the IDPCfgs are gone:

```bash
oc -n mas-{instance}-core rollout restart deploy/{instance}-coreidp deploy/{instance}-coreidp-login deploy/{instance}-coreapi
oc -n mas-{instance}-core rollout status deploy/{instance}-coreidp --timeout=240s
oc -n mas-{instance}-core rollout status deploy/{instance}-coreidp-login --timeout=240s
oc -n mas-{instance}-core rollout status deploy/{instance}-coreapi --timeout=240s
```

The `coreidp` rollout sometimes "times out" at 180s while the new pod is still spinning up — extending to 240s usually completes cleanly. If you need to confirm, watch `oc -n mas-{instance}-core get pods | grep -E "coreidp|coreapi"` until all show `1/1 Running` with `0` restarts.

### 10. Verify clean state

Report this back to the user:

```bash
oc get ns mas-est 2>&1 | head -1                                    # NotFound
oc -n mas-{instance}-core get idpcfg 2>&1 | head                    # No resources found
oc -n mas-{instance}-core get suite {instance} -o jsonpath='podTemplates={.spec.podTemplates}{"\n"}'   # podTemplates=
oc -n mas-{instance}-core get cm {instance}-selfreg --ignore-not-found 2>&1   # (empty)
oc -n mas-{instance}-core get secrets | grep usersupplied                     # only `{instance}-usersupplied-bas-creds-system` remains
```

If `--full-olm-reset` was set:

```bash
oc -n openshift-marketplace get catalogsource mas-iam-operator 2>&1 | head -1   # NotFound
```

## Common errors and what they mean

| Symptom | Cause | Fix |
|---|---|---|
| IDPCfg `delete` hangs past 5 min | `entitymgr-idpcfg` OOMKilling on finalizer | Re-apply step 2 (Suite CR memory bump); previous attempt didn't take effect |
| `MongoServerSelectionError: Hostname/IP does not match certificate's altnames` | Used short SRV form in connection string | Use `.svc.cluster.local` fully-qualified names |
| `not primary` error from mongosh | Connected to a secondary that can't write | Connection string with `replicaSet=mas-mongo-ce` routes writes to primary automatically — verify the URI |
| `MasIamStack finalizer hangs namespace deletion` | The operator inside the namespace being deleted gets killed before its finalizer completes | `oc patch masiamstack mas-est-iam -n mas-est --type=merge -p '{"metadata":{"finalizers":[]}}'` |
| Login UI still shows OIDC/SAML/LDAP after wipe | `coreidp`/`coreapi` haven't been restarted | Run step 9 |
| Post-install user-sync fails with `BMXAA10249E - Your security privileges do not allow access to add new users to DEFLTREG security group` | Stale `MAXIMO.GROUPUSER` rows from a previous install — Maximo's MEA hits a unique-key conflict on (USERID, GROUPNAME) and surfaces it as a privilege error. Misleading message. | Run step 8 (it now deletes from GROUPUSER too). If you've already installed and see this on individual users, delete just their orphan rows: `delete from MAXIMO.GROUPUSER where USERID in ('OIDC.USER1','SAML.USER1') and USERID not in (select USERID from MAXIMO.MAXUSER)` then flip those users' Mongo `sync.status` back to `PENDING`. |

## What this skill does NOT do

- Does NOT touch the MAS Suite, Manage, db2u, or coreapi deployments other than restarting coreidp/coreapi
- Does NOT delete the `mas-{instance}-core` namespace (that would break MAS itself)
- Does NOT delete the `openshift-marketplace` namespace
- Does NOT touch the `mongoce` namespace
- Does NOT delete or modify the MAS API token, SCIM bridge config, or external customer SCIM profiles
- Does NOT delete Maximo `mxe.cos*` system properties (so doclinks config survives — useful when re-validating)

If the user needs any of those wiped too, ask before doing it.
