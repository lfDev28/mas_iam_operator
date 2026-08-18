---
name: mas-est-release
description: Cut and publish a mas-est release — version bumps, doc/manifest pins, tests, git tag, and multi-arch image builds pushed to Quay (CLI image, and the SCIM bridge image when the bridge changed). Use when the user asks to ship / cut / publish / release a new version, push images to Quay, or bump to vX.Y.Z. Requires podman logged in to quay.io.
user-invocable: true
allowed-tools:
  - Bash
  - Read
  - Edit
  - Write
---

# /mas-est-release — cut and publish a mas-est release

Two independently-versioned artifacts. Publish only what changed:

| Artifact | Image | Tag scheme | Arch |
|---|---|---|---|
| CLI (`mas-est`) | `quay.io/lee_forster/mas-external-services-tool` | `v0.1.3` | multi-arch (amd64 + arm64 manifest) |
| SCIM bridge | `quay.io/lee_forster/mas-iam-scim-bridge` | `scim-bridge-v0.1.2` | single-arch `linux/amd64` |

The two version numbers are independent and drift apart — CLI `v0.1.3` shipped with bridge `scim-bridge-v0.1.2`. Never assume they match.

The operator image (`mas-iam-operator:0.0.N` / `:catalog-0.0.N`) is a third artifact with its own cadence; it is NOT part of this flow.

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

```bash
podman manifest inspect quay.io/lee_forster/mas-external-services-tool:vX.Y.Z \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); [print(m["platform"]["os"]+"/"+m["platform"]["architecture"]) for m in d["manifests"]]'
```
Must list both `linux/amd64` and `linux/arm64`. For the bridge, `podman image inspect ... --format '{{.Os}}/{{.Architecture}}'` → `linux/amd64`.

### 8. Report to the user

Give them the copy-paste upgrade line and say what needs redeploying:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:vX.Y.Z'
```

- CLI-only release → users re-run bootstrap; nothing on-cluster changes until they install.
- Bridge shipped → existing installs keep running the OLD bridge until reinstalled or the deployment image is updated. Say so explicitly; it is the most common "why isn't my fix live" confusion.

Finally, update the project memory (`project_state.md` + `MEMORY.md` index) with the new current version, both image tags, and what shipped.

## Gotchas

- **Never push without version-specific approval** (see Preconditions).
- **Bridge code without a rebuilt image is a no-op.** New env/ConfigMap keys are ignored by old images with no error.
- **Don't bump the Keycloak image tag** while bumping the bridge (§2).
- **A published tag is immutable in practice** — people have pulled it. Cut a new patch version instead of re-pushing over one.
- **Do not deploy new images to a cluster someone is actively testing on** without asking; validate on a scratch namespace or a spare cluster first.
