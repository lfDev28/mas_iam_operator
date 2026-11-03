# mas-est

mas-est (MAS External Services Toolkit) installs throwaway identity and
support services into OpenShift for IBM Maximo Application Suite: Keycloak
(OIDC/SAML), OpenLDAP, a SCIM bridge, MinIO (S3) and Mailpit (SMTP), and
wires MAS to them. User docs: `README.md`, `docs/GUIDE.md`.

## Three artifacts, versioned independently

| Artifact | Source | Image (quay.io/lee_forster/…) | Version lives in |
|---|---|---|---|
| CLI `mas-est` | `tools/mas-iam-installer` (Go), plus `scripts/`, `manifests/`, `env/` bundled into the image | `mas-external-services-tool:vX.Y.Z` | `internal/version/version.go` |
| SCIM bridge | `services/scim-bridge` (Go) | `mas-iam-scim-bridge:scim-bridge-vX.Y.Z` | `env/scim-bridge.env.release` |
| Operator + bundle + catalog | `operators/mas-iam-operator` (Helm operator) | `mas-iam-operator:0.0.N`, `:catalog-0.0.N` | `operators/mas-iam-operator/Makefile` |

`charts/mas-iam-stack` is a symlink to the operator's chart; keep it that way.
Every image the tree references is listed in `images.yaml`.

## Done means `make check` passes

`make check` runs what CI runs: go vet, go test -race and go build for both
Go modules, `bash -n` on `scripts/*.sh`, `helm lint`, the chart symlink
check, and the offline image-ref scan. A change is not done while it is red.
`make check-images` also checks every pinned image still exists on its
registry (network; nightly in CI).

## Rules

- **Releases only via the release skill** (`.claude/skills/mas-est-release`)
  and its make targets: `bump`, `release-check`, `release-images`,
  `release-verify`, `release-tag`. Never push an image, a tag or a branch
  without the user's explicit, version-specific approval.
- **Operator and bridge need their own version bump and image.** A change
  under `operators/mas-iam-operator/{helm-charts,watches.yaml,Dockerfile}` or
  `services/scim-bridge/` does nothing on a cluster until a new image is
  published under a new version (`make bump ARTIFACT=operator|bridge`).
  Publish the operator before any CLI release whose manifests reference its
  catalog tag. `make release-check` enforces this.
- **Test upgrades, not just fresh installs.** Fresh-after-wipe hides upgrade
  bugs (cluster-scoped objects survive a namespace delete, so a reinstall is
  a patch). Run both legs of `scripts/smoke.sh` before promoting an rc.
- **The in-cluster installer's RBAC is applied by the launching CLI**, not
  the Job image, and the Job image is `"v" + version.Version`. Tag and
  version.go must agree; the Containerfile refuses a mismatch.
- **This repo is public.** Never commit secrets, API keys, tokens, kubeconfigs,
  private keys or real cluster hostnames.
- Do not wipe or restart anything on a shared cluster without asking; never
  restart `coreidp` (MAS 9.1.20 SAML self-registration regression).
