# mas-est CLI

The `mas-est` CLI (Go, Cobra) installs and manages the MAS External Services on OpenShift. It orchestrates the shell install engine in `scripts/` and the manifests in `manifests/` rather than replacing them, and adds the parts that need real code: prompts and derived defaults, preflight, the in-cluster installer Job, MAS auth configuration through the MAS Admin API, MinIO, Mailpit, and support tooling.

End users install it by bootstrapping from the published image. See [docs/GUIDE.md](../../docs/GUIDE.md).

## Layout

| Path | Contents |
| --- | --- |
| `cmd/mas-iam-installer` | `main` package; the binary is named `est` inside the image, `mas-est` on the host |
| `internal/app` | One file per command (`install.go`, `install_incluster.go`, `mas_auth*.go`, `wipe.go` for `uninstall`, `bootstrap.go`, ...) |
| `internal/config` | `InstallConfig`, defaults, and the env var names each flag reads (`install.go`) |
| `internal/installer` | Runs `scripts/install-all-in-one.sh` and the uninstall script |
| `internal/masadmin` | MAS Admin API client (IDP config endpoints) |
| `internal/preflight` | Cluster, storage class, MAS endpoint, and IDPCfg overwrite checks |
| `internal/oc`, `internal/exec` | `oc` wrapper and process runner |
| `internal/version` | `Version` string; also selects the default in-cluster Job image |

Run `mas-est --help` for the command list. `bootstrap` and `render-template` are hidden.

## Behaviour worth knowing before changing code

- **In-cluster by default.** `install` runs prompts and preflight locally, then creates `ServiceAccount/mas-est-installer`, a ClusterRole/ClusterRoleBinding and Role/RoleBinding, `Secret/mas-est-install-credentials` (MAS API token, SMTP relay password), and `Job/mas-est-install`, and streams the Job logs. The Job runs `est install --non-interactive --local`. `--local` runs everything on the workstation instead. `--uninstall-first` is refused in-cluster because the Job lives in the namespace being deleted.
- **RBAC comes from the launching CLI.** The Role and ClusterRole are built in `install_incluster.go` and applied by the binary the user runs, not by the Job image. A stale local runtime applies stale RBAC even when `--installer-image` is new.
- **Version guard.** `install` prints `[version] mas-est vX.Y.Z` and fails when `MAS_EST_IMAGE` names a different tag than the binary. `--skip-version-check` overrides. The default `--installer-image` is `quay.io/lee_forster/mas-external-services-tool:v<Version>`, so the published tag must match `internal/version/version.go` exactly.
- **MAS auth provider ids.** Both `install --configure-mas-auth` and `mas-est mas-auth apply` default every provider id to `default`, giving IDPCfgs named `<instance>-<type>-default-system`. The MAS Admin UI only reports a provider as configured for that id. `install` has no flag to change it. `mas-auth apply` and `mas-auth delete` accept `--ldap-provider-id`, `--oidc-provider-id`, and `--saml-provider-id`. With `--configure-mas-auth`, preflight warns when an IDPCfg with the same name already exists.
- **Flags and env vars.** Most `install` flags can also be set through an env var, for example `MAS_EST_NAMESPACE`, `MAS_EST_COMPONENTS`, and `SCIM_BRIDGE_MAS_BASE_URL`. `internal/config/install.go` is the full list.

Design notes: [docs/design/IN-CLUSTER-INSTALLER.md](../../docs/design/IN-CLUSTER-INSTALLER.md).

## Build

```bash
cd tools/mas-iam-installer
go build -o mas-est ./cmd/mas-iam-installer
```

A locally built binary needs the repo's `scripts/`, `manifests/`, and `env/`. It finds them through `MAS_EST_REPO_ROOT`, or by walking up from the working directory or the binary's location, so running it anywhere inside the checkout works.

To build the image and test the bootstrap flow, run this from the repo root (the build context is the whole repo):

```bash
podman build -f tools/mas-iam-installer/Containerfile -t mas-est-tool:dev .
mkdir -p /tmp/mas-est-bootstrap
podman run --rm -v /tmp/mas-est-bootstrap:/tmp localhost/mas-est-tool:dev bootstrap --force
/tmp/mas-est-bootstrap/mas-est --help
```

The Containerfile cross-compiles `est` for darwin and linux on amd64 and arm64, and bundles `scripts/`, `manifests/`, and `env/`. `bootstrap` writes a `mas-est` launcher and a `.mas-est-runtime/` directory (`bin/<os>-<arch>/est`, `repo/{scripts,manifests,env}`) into the mounted directory. The launcher sets `MAS_EST_REPO_ROOT` to the bundled `repo/`. Host requirements are `oc` and bash 3.2 or later.

The runtime stage of the Containerfile uses a previously published mas-est image as its base.

## Test

```bash
cd tools/mas-iam-installer
go test ./...
```

## Release

Use the `/mas-est-release` skill ([.claude/skills/mas-est-release/SKILL.md](../../.claude/skills/mas-est-release/SKILL.md)). It covers the version bump, tests, tag, and the multi-arch image push.
