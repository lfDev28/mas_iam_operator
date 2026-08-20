---
name: mas-est-release
description: Cut and publish a mas-est release — version bumps, doc/manifest pins, tests, git tag, and image builds pushed to Quay for any of the three artifacts (CLI, SCIM bridge, operator+bundle+catalog). Use when the user asks to ship / cut / publish / release a new version, push images to Quay, bump to vX.Y.Z, or publish a dev tag for testing. Requires podman logged in to quay.io.
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Edit
  - Write
---

# /mas-est-release — cut and publish a mas-est release

Three independently-versioned artifacts. Publish only what changed:

| Artifact | Image | Tag scheme | Arch | Section |
|---|---|---|---|---|
| CLI (`mas-est`) | `quay.io/lee_forster/mas-external-services-tool` | `v0.1.3` | multi-arch (amd64 + arm64 manifest) | §6 |
| SCIM bridge | `quay.io/lee_forster/mas-iam-scim-bridge` | `scim-bridge-v0.1.2` | single-arch `linux/amd64` | §6 |
| Operator (+bundle+catalog) | `quay.io/lee_forster/mas-iam-operator` | `0.0.15` / `catalog-0.0.15` | single-arch `linux/amd64` | §9 |

The version numbers are independent and drift apart — CLI `v0.1.3` shipped with bridge `scim-bridge-v0.1.2` and operator `0.0.14`. Never assume they match.

**Sequencing trap (bit us 2026-08-18):** bumping the operator version in the repo points `manifests/install-olm.yaml` and `scripts/install-operator-from-catalog.sh` at a `catalog-0.0.N` that does not exist on Quay yet. Any install from that commit hangs and fails at the CatalogSource wait. If the operator version was bumped, **publish the operator before cutting a CLI release that bundles those manifests.**

## Dev tags

For changes that need real-cluster iteration before earning a release number (RBAC, anything untested in-cluster), publish a dev tag instead:

- Set `version.go` to `0.1.4-dev` and publish the CLI image as `v0.1.4-dev`. **These must match**: the in-cluster installer derives its default Job image from `"v" + version.Version`, so a mismatch silently launches the wrong image.
- Leave doc pins on the last stable release — docs should not advertise a dev build.
- No git tag for dev builds; commit only.
- Iterate on `-dev` freely, then bump to the real version and follow the full flow.

## Preconditions

1. **Named approval to push.** Each Quay push needs the user's explicit go-ahead for that specific version ("push v0.1.3", "ship it"). Never push on your own initiative.
2. `podman login --get-login quay.io` returns a user. If not, ask them to run `podman login quay.io` — do not attempt to authenticate for them.
3. Working tree has the changes to ship, `main` is the release branch, tests pass.

## Steps

### 1. Decide what changed

```bash
git log --oneline $(git describe --tags --abbrev=0)..HEAD
```

- Anything under `tools/mas-iam-installer/` → new CLI version.
- Anything under `services/scim-bridge/` → new bridge image. **A bridge code change does nothing without a rebuilt image** — an old image silently ignores new ConfigMap keys.
- Only `scripts/`, `manifests/`, `docs/` → usually still ship a CLI version, since the CLI image bundles scripts and manifests.

### 2. Version bumps

CLI version string:
- `tools/mas-iam-installer/internal/version/version.go` → `Version = "0.1.3"` (no `v` prefix here).

CLI image pins in docs (all `mas-external-services-tool:vX.Y.Z`):
```bash
for f in README.md tools/mas-iam-installer/README.md docs/BETA-ANNOUNCEMENT.md \
         docs/BETA-INSTALL-TUTORIAL.md docs/MAS-EST-USER-GUIDE.md \
         docs/DEMO-SCRIPT.md docs/BETA-QUICKSTART.md; do
  sed -i '' 's|mas-external-services-tool:vOLD|mas-external-services-tool:vNEW|g' "$f"
done
```
(`docs/INITIAL-RELEASE-PLAN.md` is a historical record — leave it pinned.)

Bridge image pins (only when the bridge ships):
```bash
for f in env/scim-bridge.env.release manifests/scim-bridge-install-template.yaml \
         scripts/install-all-in-one.sh manifests/scim-bridge-install.yaml; do
  sed -i '' 's|mas-iam-scim-bridge:scim-bridge-vOLD|mas-iam-scim-bridge:scim-bridge-vNEW|g' "$f"
done
```
**Careful:** `manifests/scim-bridge-install-template.yaml` also pins `mas-iam-keycloak:scim-bridge-vX.Y.Z` — a *different* image that happens to share the tag naming. Match on the full `mas-iam-scim-bridge:` repo prefix so you don't bump Keycloak by accident. Verify with `grep -rn "scim-bridge-v"` afterwards.

### 3. Release notes

Prepend a block to the `## What Is Supported` section of `docs/BETA-KNOWN-LIMITATIONS.md`, newest first, naming both artifact versions when both ship:

```markdown
**v0.1.3 release notes** (CLI `v0.1.3` + SCIM bridge image `scim-bridge-v0.1.2`):
- <user-visible change, why it mattered, and any behavior change to expect>
```

Write for a support engineer hitting the bug, not a changelog robot: name the symptom and the workaround where relevant.

### 4. Test both modules

```bash
cd tools/mas-iam-installer && go build ./... && go test ./...
cd ../../services/scim-bridge && go build ./... && go test ./...
```
Both must be green before tagging. Also `bash -n` any shell script touched.

### 5. Commit, tag, push

```bash
git add -A
git commit -m "Release vX.Y.Z — <one-line summary>"
git tag vX.Y.Z
git push origin main vX.Y.Z
```
Tag names track the CLI version. The bridge image tag is recorded in the commit body, not as a separate git tag.

### 6. Build and push images

Long-running (several minutes per arch) — run in the background and report when the manifest verifies.

CLI, multi-arch (per-arch tags then a manifest list, so a laptop pulling `:vX.Y.Z` gets its own arch):
```bash
IMG=quay.io/lee_forster/mas-external-services-tool
for arch in amd64 arm64; do
  podman build --platform linux/$arch -f tools/mas-iam-installer/Containerfile -t $IMG:vX.Y.Z-$arch .
done
podman push $IMG:vX.Y.Z-amd64
podman push $IMG:vX.Y.Z-arm64
podman manifest rm $IMG:vX.Y.Z 2>/dev/null || true
podman manifest create $IMG:vX.Y.Z
podman manifest add $IMG:vX.Y.Z docker://$IMG:vX.Y.Z-amd64
podman manifest add $IMG:vX.Y.Z docker://$IMG:vX.Y.Z-arm64
podman manifest push --all $IMG:vX.Y.Z docker://$IMG:vX.Y.Z
```
Run from the repo root — the Containerfile's build context is the whole repo (it copies `scripts/`, `manifests/`, `env/`).

Note: the CLI Containerfile's runtime stage is `FROM` a *previous published mas-est image* (self-referencing base, pinned to `v0.1.0-beta.9`). It only supplies OS layers plus the bootstrap layout; everything else is rebuilt. Dropping this self-reference for a small OS base is a tracked roadmap item.

Bridge, single-arch (it only ever runs on the cluster; the build script also pushes):
```bash
SCIM_BRIDGE_IMAGE=quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-vX.Y.Z \
SCIM_BRIDGE_TARGET_ARCH=amd64 \
SCIM_BRIDGE_CONTAINER_CLI=podman \
bash scripts/scim-bridge-01-build-image.sh
```
Set `SCIM_BRIDGE_TARGET_ARCH` explicitly — unset, the script infers it from the *currently logged-in cluster's* nodes, which silently produces the wrong arch when pointed at an arm64 cluster or none at all.

### 7. Verify published

Multi-arch CLI image — `manifest inspect` works because it *is* a manifest list:
```bash
podman manifest inspect quay.io/lee_forster/mas-external-services-tool:vX.Y.Z \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); [print(m["platform"]["os"]+"/"+m["platform"]["architecture"]) for m in d["manifests"]]'
```
Must list both `linux/amd64` and `linux/arm64`.

Single-arch images (bridge, operator, catalog) are **not** manifest lists, so
`podman manifest inspect` fails on them and looks exactly like "image missing".
Pull from the registry instead — this also proves the push actually landed
rather than inspecting a stale local copy:
```bash
podman pull -q quay.io/lee_forster/mas-iam-operator:catalog-0.0.15 >/dev/null \
  && podman image inspect quay.io/lee_forster/mas-iam-operator:catalog-0.0.15 --format '{{.Os}}/{{.Architecture}}'
```
`skopeo` is **not installed** on this machine — a `skopeo inspect` check silently
falls through to whatever `||` branch follows it and reports a false "MISSING".

### 8. Report to the user

Give them the copy-paste upgrade line and say what needs redeploying:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:vX.Y.Z'
```

- CLI-only release → users re-run bootstrap; nothing on-cluster changes until they install.
- Bridge shipped → existing installs keep running the OLD bridge until reinstalled or the deployment image is updated. Say so explicitly; it is the most common "why isn't my fix live" confusion.

Finally, update the project memory (`project_state.md` + `MEMORY.md` index) with the new current version, both image tags, and what shipped.

### 9. Operator release (image + bundle + catalog)

Only when something under `operators/` changed. Three images, all `linux/amd64`.
Version lives in `operators/mas-iam-operator/Makefile` (`VERSION ?= 0.0.N`) and
must also be bumped in `config/manager/kustomization.yaml`, the bundle CSV
(`name`, `image`, `version`), `manifests/install-olm.yaml`,
`scripts/install-operator-from-catalog.sh`, and `docs/MAS-EST-USER-GUIDE.md`.

`operators/mas-iam-operator/README.md` documents a one-liner using
`make docker-build docker-push bundle bundle-build bundle-push catalog-build catalog-push`.
**Do not use it as-is on an arm64 Mac.** `docker-build` and `bundle-build` both
run `$(CONTAINER_ENGINE) build` with no `--platform`, and `opm index add` builds
the catalog through podman the same way — so on Apple Silicon all three images
publish as arm64. The cluster is amd64, and the failure surfaces later as the
operator pod crash-looping with an exec-format error, which reads like "the new
operator version is broken" rather than "wrong architecture".

Force the platform at every step (verified 2026-08-20 for 0.0.15):

```bash
cd operators/mas-iam-operator
VERSION=0.0.15
IMG=quay.io/lee_forster/mas-iam-operator:${VERSION}
BUNDLE_IMG=quay.io/lee_forster/mas-iam-operator-bundle:${VERSION}
CATALOG_IMG=quay.io/lee_forster/mas-iam-operator:catalog-${VERSION}

podman build --platform linux/amd64 -t "$IMG" .
podman push "$IMG"

CONTAINER_ENGINE=podman VERSION="$VERSION" IMG="$IMG" make bundle

podman build --platform linux/amd64 -f bundle.Dockerfile -t "$BUNDLE_IMG" .
podman push "$BUNDLE_IMG"

make opm
./bin/opm index add --container-tool podman --mode semver \
  --tag "$CATALOG_IMG" --bundles "$BUNDLE_IMG" \
  --generate --out-dockerfile index.Dockerfile
podman build --platform linux/amd64 -f index.Dockerfile -t "$CATALOG_IMG" .
podman push "$CATALOG_IMG"
```

`--generate` makes `opm` emit the Dockerfile instead of building it, which is the
only way to control the catalog image's architecture.

Note the catalog tag is `mas-iam-operator:catalog-0.0.N` — the repo's manifests
expect that, **not** the Makefile's `CATALOG_IMG` default of
`mas-iam-operator-catalog:v0.0.N`. Always pass `CATALOG_IMG` explicitly.

Verify all three per §7, then confirm on-cluster that the CatalogSource reaches
`READY` and `packagemanifests` reports the new `currentCSV` before declaring the
operator released.

## Gotchas

- **Never push without version-specific approval** (see Preconditions).
- **Publish the operator before a CLI release that bundles bumped manifests** — otherwise installs fail at the CatalogSource wait against a catalog tag that does not exist.
- **Force `--platform` on every operator/bridge build** on an arm64 host; only the CLI build loop sets it already.
- **`podman manifest inspect` on a single-arch image reports failure, not absence** — use the pull-then-inspect check in §7.
- **Bridge code without a rebuilt image is a no-op.** New env/ConfigMap keys are ignored by old images with no error.
- **Don't bump the Keycloak image tag** while bumping the bridge (§2).
- **A published tag is immutable in practice** — people have pulled it. Cut a new patch version instead of re-pushing over one.
- **Do not deploy new images to a cluster someone is actively testing on** without asking; validate on a scratch namespace or a spare cluster first.
