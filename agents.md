# Agents & Responsibilities

| Agent    | Scope                                         | Responsibilities                                                                                              |
| -------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Platform | Operators, Helm charts, OpenShift integration | Owns T1, T2, T5, T6. Keeps the chart single-sourced, updates RBAC/SCC handling, and enforces secure defaults. |
| Security | TLS assets, secret management                 | Owns T3. Builds the hardened TLS generator image and ensures credentials never leak via logs.                 |
| Docs     | Install guides, manifests, samples            | Owns T4. Splits the install manifest, refreshes README snippets, and communicates the new flow.               |
| DevEx    | Tooling, scripts, repo hygiene                | Owns T7, T8. Maintains reset scripts, cleans tracked artifacts, and curates `.gitignore` / helper tooling.    |

Each agent should update `tasks.json` when a task’s status changes and note any cross-team dependencies inside commit messages or PR descriptions.

## CI guardrails

- Run `make lint` (wired for CI) to ensure `charts/mas-iam-stack` stays pointed at `operators/mas-iam-operator/helm-charts/mas-iam-stack`.
- Helm edits should flow through the operator copy of the chart; the root-level path is now a symlink for local convenience only.
- GitHub Actions (`.github/workflows/lint.yml`) now enforces this guard on every push/PR.
