# MAS IAM Hardening Plan

## Phase 1 – Unblock installs (T1–T3)
1. Consolidate the Helm chart source so the operator and local deployments consume the same templates. Add a CI guard (e.g., checksum check) to prevent drift.
2. Fix operator RBAC generation and update the bundle/CSV, ensuring the service account can manage RoleBindings and request the anyuid SCC without manual `oc adm` calls.
3. Rebuild the TLS bootstrap flow with a pre-hardened image, narrowed RBAC, and non-logged passwords.

Deliverable: a fresh bundle + install manifest that succeeds end-to-end on OpenShift without extra commands.

## Phase 2 – Security posture & UX (T4–T7)
1. Split the install manifest so optional sample CRs are applied only after the operator is healthy.
2. Remove hard-coded credentials from the chart/CR defaults and require caller-provided Secrets.
3. Enable persistent/OpenShift-safe defaults for OpenLDAP (persistence, runAsNonRoot, requests, SCC guidance).
4. Align `reset-namespace.sh` behavior with its documentation (or expose a flag for TLS purging).

Deliverable: opinionated defaults that follow security best practices and updated docs/scripts so day-two tasks are predictable.

## Phase 3 – Repo hygiene & automation (T8 + follow-ons)
1. Clean tracked artifacts and extend `.gitignore`.
2. Add lightweight CI (-lint Helm chart, validate manifests/install-olm split, maybe kind-based smoke test).
3. Consider introducing release automation (Make targets or GitHub Actions) so catalog + bundle builds stay consistent.

Deliverable: tidy repo plus guardrails that keep future contributions aligned with the agreed conventions.
