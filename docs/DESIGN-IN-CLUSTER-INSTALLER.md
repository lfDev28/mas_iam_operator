# Design: In-Cluster Installer ("mas-est-inst" Job)

Status: proposed. Owner: mas-est. Origin: Lee's maxinst-style installer idea, plus
two install failures this week caused by the engineer's laptop losing the session
mid-run (sleep / network drop) — not by anything wrong with the install itself.

## Problem

`mas-est install` runs from the engineer's laptop and drives the cluster over `oc`
for 15–30 minutes. The laptop is a single point of failure for the whole run:
sleeping the lid, dropping VPN, or closing the terminal kills it mid-phase, and
the terminal is blocked for the duration. Retry/backoff inside the CLI does not
help — the outage is minutes-to-hours of no connectivity, not a blip.

Maximo Manage solves the same problem with `maxinst`: a pod does the long work,
the operator watches logs and can walk away. MAS core installs likewise run as
Tekton pipelines in-cluster.

## Goal

Launch the install in-cluster, detach at will, reattach for logs, and have it
survive the laptop entirely. No new published artifact.

## Why a Job (recommended) over the alternatives

The mas-est image is already a self-contained runnable unit. Verified against
`quay.io/lee_forster/mas-external-services-tool:v0.1.3`:

- `ENTRYPOINT ["est"]`, with `MAS_EST_REPO_ROOT=/opt/mas-est`, so `repoScriptPath`
  resolves the bundled `scripts/` and `manifests/` without a bootstrap step.
- Ships `oc`, `bash`, and `curl`. It does **not** ship `mongosh` and does not need
  it — `link-scim-users-oidc.sh` runs `oc exec … -- mongosh` inside the MAS Mongo
  pod.
- `est install --non-interactive` already exists and is the path used for every
  scripted install to date.
- `ui.IsInteractive()` is TTY-based (`survey.go:16`), so prompts disable
  themselves in a pod with no code change.

So a Job needs: a ServiceAccount + RBAC, a ConfigMap of settings, a Secret for the
MAS API token, and a Job spec. Nothing else.

**Tekton** buys per-step granularity, per-task retry, and the OpenShift console
UI, and it is the idiom MAS engineers already know. It costs a hard dependency on
the OpenShift Pipelines operator (present on some clusters, not guaranteed), a set
of Task/Pipeline resources to maintain in lockstep with the CLI, and much harder
local testing. It also does not change the core win — moving execution
in-cluster. A Pipeline can wrap the same binary later; choosing a Job now does not
foreclose it.

**Extending the mas-iam-operator** (a new CR the operator reconciles) fits
Kubernetes idiom and gives status conditions for free, but the operator is
Helm-based and today owns only the IAM stack. Install involves imperative,
ordered, cluster-wide work — MAS Admin API calls, IDPCfg PUTs, Mongo writes —
which is a poor fit for a Helm reconcile loop and a large lift.

## Design

### Resources (all created by the CLI, all in the target namespace except RBAC)

| Resource | Purpose |
|---|---|
| `ServiceAccount/mas-est-installer` | Job identity |
| `ClusterRole/ClusterRoleBinding` | Permissions below |
| `ConfigMap/mas-est-install-config` | Non-secret install settings (components, namespaces, hosts, storage classes, providers) |
| `Secret/mas-est-install-credentials` | MAS API token name/value |
| `Job/mas-est-install` | Runs `est install --non-interactive …` |

The Job consumes config via the env vars the CLI already honours
(`SCIM_BRIDGE_*`, `MAS_EST_*` — see `config.LoadInstallConfigFromEnv`), so the
container command stays short and no new config format is invented.

### CLI surface

- `mas-est install --in-cluster` — resolves config exactly as today (prompts on
  the laptop, where prompting is pleasant), then creates the resources above and
  streams the Job's logs. **Ctrl-C detaches; it does not cancel the install.**
- `mas-est logs --install-job` — reattach to a running or finished Job.
- `mas-est status` — reports Job phase alongside the existing component status.
- `mas-est uninstall` — also removes the installer Job/RBAC/config.

### RBAC

Install touches a lot. Enumerated so we can ship an explicit ClusterRole rather
than binding `cluster-admin`:

- cluster-scoped: `namespaces` (create), `customresourcedefinitions` (get/create —
  `masiamstacks`), `storageclasses` (list), `securitycontextconstraints` (the
  existing `anyuid` binding for the OpenLDAP TLS generator, see
  `manifests/install-olm.yaml`)
- OLM: `catalogsources`, `operatorgroups`, `subscriptions`, `clusterserviceversions`,
  `packagemanifests`
- `mas-est` namespace: full CRUD on `secrets`, `configmaps`, `deployments`,
  `statefulsets`, `jobs`, `services`, `routes`, `pvcs`, `masiamstacks`
- MAS core namespace: `idpcfgs` (create/update), `suites` (get/patch — the
  entitymgr memory bump), `secrets` (get/create), `configmaps` (the `{instance}-selfreg`
  map), `routes` (get)
- Mongo namespace: `pods` (list) and **`pods/exec`** — required by the SCIM
  identity linker

`pods/exec` plus cross-namespace secret access is close to cluster-admin in
practice. Ship the explicit role as the default, document it, and let operators
substitute their own.

### Known blockers to solve during implementation

1. **Run-log path is not writable.** `logging.OpenRunLog` writes to
   `{repoRoot}/.mas-est-installer/logs` (`logfile.go:11`) — i.e. `/opt/mas-est/…`
   in the image. Under OpenShift's default `restricted-v2` SCC the pod gets an
   arbitrary UID that cannot write there. Fix: honour a log-dir override
   (env or flag) and point it at an `emptyDir` mounted at `/tmp`, or fall back to
   stdout-only when the directory is unwritable. **The Job must not fail because
   it cannot open a log file** — stdout is the real log here anyway.
2. **Arbitrary UID generally.** The image declares `USER root`; scripts that
   assume a writable `$HOME` need the same treatment the Keycloak bootstrap
   already uses (`export HOME=/tmp/...`). Audit before shipping.
3. **In-cluster auth.** `ensureClusterLogin` (non-interactive branch) requires
   `currentClusterContext` to succeed. With the SA token mounted, `oc whoami`
   resolves in-cluster — verify end-to-end, and ensure the interactive `oc login`
   fallback can never trigger in a pod.
4. **Config completeness.** `--non-interactive` fails fast on missing values, so
   the CLI must resolve every required value before creating the Job. This is
   already how the flag behaves; the in-cluster path just makes it mandatory.
5. **Secret hygiene.** The MAS API token goes in a Secret consumed via `envFrom` —
   never in the ConfigMap or inline in the Job spec, both of which show up in
   `oc get -o yaml`.

### Job spec decisions

- `restartPolicy: Never`, `backoffLimit: 0`. The install is idempotent, but a
  blind restart silently repeats 20+ minutes of work; a human deciding to re-run
  is better than a controller doing it.
- `activeDeadlineSeconds: 5400` (90 min) — well clear of the slowest observed run
  (MAS 9.1.4 IDPCfg reconciles at ~7 min each) while still bounding a hang.
- `ttlSecondsAfterFinished` unset initially: the Job and its logs are the
  post-mortem artifact. Cleanup happens on the next `--in-cluster` run or via
  `mas-est uninstall`.

### Phasing

1. **Phase 1** — Job manifest + `--in-cluster` + log streaming. Delivers the whole
   point (survive the laptop) and is testable on any cluster.
2. **Phase 2** — progress reporting beyond the log stream: the existing phase
   runner writes phase/elapsed into a status ConfigMap so `mas-est status` can say
   "3/4: Configure MAS auth" without parsing logs. Plus `logs --install-job`
   reattach.
3. **Phase 3 (optional)** — Tekton Pipeline wrapping the same binary for step
   granularity and console UI, for clusters that already run Pipelines.

## Open questions

- Two engineers installing against one cluster: namespace-scope the Job name, or
  refuse when one is already running?
- Should `--in-cluster` become the default once proven, with the local path kept
  as `--local` for development?
- Support bundle on failure: auto-collect into a ConfigMap/PVC, or leave to the
  existing `mas-est support-bundle` command run from the laptop?
