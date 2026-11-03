---
name: mas-est-release
description: Cut and publish a mas-est release — version bumps, release checks, a release candidate, a cluster smoke test, promotion, and git tags last, for any of the three artifacts (CLI, SCIM bridge, operator+bundle+catalog). Use when the user asks to ship / cut / publish / release a new version, push images to Quay, bump to vX.Y.Z, publish a release candidate, or publish a dev tag for testing. Requires podman logged in to quay.io.
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Edit
  - Write
---

# /mas-est-release — cut and publish a mas-est release

The mechanics live in scripts behind make targets. This skill is the order,
the approvals, and the traps. **Run the steps in order and stop at the first
red; report it, do not work around it.**

| Artifact | Image (quay.io/lee_forster/…) | Tags | Arch | Git tag |
|---|---|---|---|---|
| CLI | `mas-external-services-tool` | `vX.Y.Z-rc.N`, `vX.Y.Z` | amd64 + arm64 manifest list | `vX.Y.Z` |
| SCIM bridge | `mas-iam-scim-bridge` | `scim-bridge-vX.Y.Z` | amd64 | `scim-bridge-vX.Y.Z` |
| Operator | `mas-iam-operator`, `-bundle` | `0.0.N`, `catalog-0.0.N` | amd64 | `operator-0.0.N` |

Versions are independent and drift apart; never assume they match.

## Preconditions

1. **Named approval for every push.** Each `PUSH=1` below needs the user's
   explicit go-ahead for that artifact and version ("push v0.1.16-rc.1").
   Never push on your own initiative; an approval for an rc is not one for
   the promotion or the tags.
2. `podman login --get-login quay.io` prints a user. If not, ask the user to
   run `podman login quay.io`; never authenticate for them.
3. On `main`, tree clean, `make check` green.

## Steps

1. **What changed.** `git log --oneline $(git describe --tags --abbrev=0 --match 'v*')..HEAD`
   - `operators/mas-iam-operator/{helm-charts,watches.yaml,Dockerfile}` → operator bump.
   - `services/scim-bridge/**` → bridge bump. Bridge code without a new image is a no-op on the cluster.
   - Anything else (scripts, manifests, env, Go) → CLI version; the CLI image bundles scripts and manifests.
2. **Bump**, operator and bridge first (the CLI bump writes a RELEASE-NOTES
   skeleton naming the current bridge and operator):
   `make bump ARTIFACT=operator VERSION=0.0.N`, `make bump ARTIFACT=bridge VERSION=X.Y.Z`,
   `make bump ARTIFACT=cli VERSION=X.Y.Z`. Then fill in the `## vX.Y.Z` bullets
   (symptom + any user action, one or two sentences each), update
   `docs/TROUBLESHOOTING.md`, `docs/KNOWN-LIMITATIONS.md` and `docs/GUIDE.md`
   for what changed, run `make docs-pdf`, and commit `Release vX.Y.Z — <summary>`.
3. **Publish operator / bridge first, if bumped** (approval):
   `make release-images ARTIFACT=operator VERSION=0.0.N PUSH=1`,
   `make release-images ARTIFACT=bridge VERSION=X.Y.Z PUSH=1`.
   The CLI's manifests point at `catalog-0.0.N`; if that tag is missing,
   every install hangs at the CatalogSource wait. They get no rc tag:
   nothing installs them until a CLI release references them.
4. **Gate:** `make release-check VERSION=X.Y.Z [OPERATOR_VERSION=0.0.N] [BRIDGE_VERSION=X.Y.Z]`.
   Read the warnings too. A missing `operator-*` / `scim-bridge-v*` git tag
   is reported with the command to create it; pass that to the user.
5. **Release candidate** (approval): `make release-images ARTIFACT=cli VERSION=X.Y.Z-rc.1 PUSH=1`.
   version.go stays `X.Y.Z`; the Containerfile stamps `-rc.1` and fails the
   build for any other mismatch. `mas-est update` and `scripts/get-mas-est.sh`
   only pick `vX.Y.Z` tags, so users never see an rc.
6. **Smoke** on a scratch cluster (ask first; never a cluster someone is
   testing on): `scripts/smoke.sh --image vX.Y.Z-rc.1 --leg both`.
   Both legs matter: fresh-after-wipe alone hides upgrade bugs. Red → fix,
   commit, back to step 4 with `-rc.2`.
7. **Promote** (approval): same commit, no new changes:
   `make release-images ARTIFACT=cli VERSION=X.Y.Z PUSH=1`. It verifies from
   the registry, including that the image's revision label is HEAD.
8. **Tag last** (approval): `make release-tag VERSION=X.Y.Z [OPERATOR_VERSION=..] [BRIDGE_VERSION=..] PUSH=1`.
   Re-verifies every image, then creates and pushes `vX.Y.Z`, `operator-0.0.N`
   and `scim-bridge-vX.Y.Z` (existing tags are left alone) with the branch.
9. **Report:** give `export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:vX.Y.Z'`.
   If the bridge shipped, say that existing installs keep the old bridge until
   reinstalled (the most common "why isn't my fix live"). Update the project
   memory (`project_state.md`, `MEMORY.md`).

## Dev tags

For changes that need cluster iteration before they earn a version:
`make bump ARTIFACT=cli VERSION=X.Y.Z-dev` (version.go only; docs stay on the
last release), commit, `make release-images ARTIFACT=cli VERSION=X.Y.Z-dev PUSH=1`
(approval). No git tag. `-dev` tags may be re-pushed; release and rc tags may
not. The in-cluster installer runs the Job image `"v" + version.Version`, so
the tag and version.go must agree; the Containerfile enforces it.

## Traps (the scripts handle these; know them when a step goes red)

- **Operator before CLI.** See step 3. `make check-images` (nightly in CI,
  and in release-check) fails while a pinned tag is missing.
- **Force `--platform`.** On an arm64 Mac, an unforced build publishes arm64
  images that crash-loop on the amd64 cluster with an exec-format error,
  which reads like a broken release. release-images forces it everywhere. Do
  not use the operator Makefile one-liner (`docker-build … catalog-push`):
  it does not force the platform, and its `CATALOG_IMG` default is not the
  `mas-iam-operator:catalog-0.0.N` tag the manifests expect.
- **Multi-arch manifest.** A plain local image under the manifest name (left
  by any pull or run of that tag) makes `podman manifest create` fail with
  "image is not a manifest list" while the per-arch pushes succeed, and the
  multi-arch tag silently keeps the previous build. The script removes it
  first and re-verifies from the registry. Never trust push output.
- **Verify from the registry, not podman.** Single-arch images are not
  manifest lists, so `podman manifest inspect` reports them as missing.
  `skopeo` is not installed. Use `make release-verify` or
  `scripts/check-images.sh --ref <ref> --archs amd64`.
- **Bridge pins** share the `scim-bridge-vX.Y.Z` naming with the
  `mas-iam-keycloak` image; bump-version matches the full
  `mas-iam-scim-bridge:` repo so Keycloak is never bumped.
- **Published tags are immutable.** People have pulled them. Cut a new patch
  or rc number; release-images refuses to overwrite.
- **CLI runtime base** is `mas-external-services-tool:v0.1.0-beta.9`
  (self-referencing). If that tag goes, no CLI can be built; images.yaml
  checks it nightly.
- **Operator on-cluster check:** after an operator release, the CatalogSource
  must reach READY and `packagemanifests` must report the new `currentCSV`;
  the smoke test asserts this.
