# Design: Group-Based SCIM Bridge Scoping

Status: phase 1 and phase 2 implemented (target v0.2.0). Owner: mas-est. Origin: field feedback 2026-08-18 —
a support engineer added a user to the `mas-scim-users` Keycloak group and expected
the bridge to sync it; nothing happened because scoping is username-prefix-based.

## Problem

The bridge decides which Keycloak users to sync into MAS with two filters
(`SCIM_BRIDGE_INCLUDE_USERNAMES`, `SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX`, default
`scim.`). The `mas-scim-users` group that `configure-demo-users.sh` creates is
purely organizational — the bridge never reads group membership. This surprises
users (the group looks like the contract), forces a naming convention onto real
directories, and inherits `ListUsers`' single-page 50-user cap.

The prefix exists for a reason: syncing `oidc.*`/`saml.*` users would pre-create
them in MAS and break their first-login self-registration (create-only → 409).
Any replacement must preserve an explicit opt-in boundary.

## Proposal

Add group-based scoping as the preferred mode, keeping the existing filters for
backward compatibility:

- `SCIM_BRIDGE_INCLUDE_GROUPS` (comma-separated Keycloak group names or paths,
  e.g. `mas-scim-users`). When set, the poller sources its user list from group
  membership instead of `ListUsers`:
  1. Resolve each name → group id via `GET /admin/realms/{realm}/groups?search=`
     (exact-match on name/path; fail the cycle loudly on an unresolvable name —
     a typo must not silently sync nobody).
  2. Page through `GET /admin/realms/{realm}/groups/{id}/members` (`first`/`max`
     loop until a short page) — this also fixes the 50-user cap for the scoped set.
  3. Union + dedupe members across groups (by user id).
- Composition: when both groups and username filters are set, groups select the
  candidate set and the username filters further narrow it (AND). Existing
  installs (prefix only) behave exactly as today.
- Installer: `mas-est install` keeps seeding `mas-scim-users` and switches the
  default scope to `SCIM_BRIDGE_INCLUDE_GROUPS=mas-scim-users` with the prefix
  retained as belt-and-braces during one release, then prefix default drops.

### Client work (`services/scim-bridge/internal/keycloak`)

New methods `ListGroups(ctx, search)` and `ListGroupMembers(ctx, groupID, first, max)`
on the existing client (same token/TLS plumbing). Members payload matches the
users payload, so `keycloak.User` mapping is reused; the federated-user filter
(`TestListUsersFiltersFederatedUsersByDefault`) must apply to members too.

### Phasing

- **Phase 1 (implemented): scoping only.** The planner only has Create/Update;
  a user removed from the group is no longer reconciled (left as-is in MAS),
  matching today's behavior when a user stops matching the prefix. Phase 1 must
  make the resolved scoped set an explicit value in the poller so phase 2 can
  diff it against the bridge's state store.
- **Phase 2 (implemented): deactivate-on-removal.**
  A user present in the bridge state store but absent from the scoped set gets
  deactivated in MAS (SCIM PATCH `active: false` — not delete) and tombstoned
  in state (`status: deactivated`) so the deactivation fires once. Transient
  Keycloak errors must not mass-deactivate: any group-resolve or member-list
  failure hard-fails the cycle before removal detection runs, so an empty
  scoped set only ever means the groups are genuinely empty (valid — it
  deactivates every tracked user, behind a warn-level log line with counts).
  Rejoining the group clears the tombstone and reactivates via the normal
  update path; a failed deactivation records `status: error` without a
  tombstone so the next cycle retries it.
  - **Group mode only (hard constraint).** Removal detection runs *only* when
    `SCIM_BRIDGE_INCLUDE_GROUPS` is set. Group membership is paged fully, so
    the scoped set is complete and safe to diff against state. The legacy
    prefix/`ListUsers` mode fetches a single 50-user page — diffing state
    against that page would mass-deactivate every tracked user beyond page
    one, so it never feeds removal detection.
  - **Poller only.** Backfill is a bootstrap sweep that seeds correlations;
    a one-shot backfill against a partially-populated group must not
    deactivate anyone, so removal detection lives in the steady-state poller.

### Explicit non-goals

- **No nested-group flattening.** Keycloak's members endpoint returns direct
  members only; subgroups need their own entry in `SCIM_BRIDGE_INCLUDE_GROUPS`.
- **No hard deletes in MAS** — deactivation only, in phase 2.

## Interactions

- **Late-user linker gap** gets *more* visible with group scoping (adding a user
  to a group is a one-click action that today still leaves the MAS record
  `_local`-only until the linker re-runs). The post-provision identity
  remediation roadmap item (v0.1.x) should land with or before this.
- **Group-based profile routing** (existing v0.2.0 idea, `profile_resolver.go`):
  same group-membership data — build the membership fetch once, feed both.

## Testing

- httptest-backed client tests for group resolve + member pagination (multi-page).
- Poller tests: group scope alone, group∧prefix composition, unresolvable group
  name → error, dedupe across overlapping groups.
- Live validation on a demo cluster: add user to group → next poll syncs; remove
  → no action; user in group with non-matching prefix while prefix set → skipped.
