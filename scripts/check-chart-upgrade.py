#!/usr/bin/env python3
"""Offline upgrade check for the mas-iam-stack chart.

Upgrade bugs have reached users because releases were tested fresh-after-wipe:
a field Kubernetes refuses to change, or an object a new chart no longer
renders (Helm deletes it, along with any data in a PVC), only shows up when
the operator runs `helm upgrade` over an existing install.

This renders the chart the way the operator does, with the MasIamStack spec
from manifests/install-olm-sample.yaml plus the fields install-olm-sample.sh
injects, and compares:

  base chart + base values  ->  head chart + base values   (OLM upgraded the
                                                            operator; the CR
                                                            is unchanged)
  base chart + base values  ->  head chart + head values   (full upgrade)

Base is the last release tag (vX.Y.Z, no rc) unless --base is given; head is
the working tree. It reports, per transition:

  ERROR  an immutable field changed (Deployment/StatefulSet/Job selector,
         StatefulSet volumeClaimTemplates/serviceName/podManagementPolicy,
         PVC storageClassName/accessModes/volumeMode/a size decrease, Job
         template, RoleBinding roleRef, Secret/ConfigMap type or immutable
         data, Service clusterIP/type ExternalName);
  ERROR  a PVC, Secret or ConfigMap holding state disappears without
         helm.sh/resource-policy: keep (Helm deletes it on upgrade);
  WARN   any other object disappears (Helm deletes it on upgrade).

Helm hooks are skipped: Helm recreates them instead of patching them.

Needs helm, git and PyYAML. Exit 1 on any ERROR.
"""

from __future__ import annotations

import argparse
import copy
import io
import os
import subprocess
import sys
import tarfile
import tempfile

try:
    import yaml
except ImportError:  # pragma: no cover
    sys.exit("check-chart-upgrade: PyYAML is required (pip install pyyaml)")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHART = "operators/mas-iam-operator/helm-charts/mas-iam-stack"
SAMPLE = "manifests/install-olm-sample.yaml"
RELEASE = "mas-est-iam"  # helm-operator uses the CR name as the release name
NAMESPACE = "mas-est"
STORAGE_CLASS = "upgrade-check-sc"
API_VERSIONS = [
    "security.openshift.io/v1/SecurityContextConstraints",
    "security.openshift.io/v1",
    "route.openshift.io/v1",
    "route.openshift.io/v1/Route",
]
STATEFUL_KINDS = {"PersistentVolumeClaim", "Secret", "ConfigMap"}


def run(cmd: list[str], **kw) -> str:
    res = subprocess.run(cmd, capture_output=True, text=True, **kw)
    if res.returncode != 0:
        raise RuntimeError(f"{' '.join(cmd)} failed:\n{res.stderr.strip()}")
    return res.stdout


def last_release_tag() -> str:
    return run(["git", "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "--exclude", "*-*"], cwd=ROOT).strip()


def extract(ref: str, dest: str) -> str:
    """Export the chart and sample manifest at ref into dest; return dest."""
    data = subprocess.run(["git", "archive", ref, CHART, SAMPLE], cwd=ROOT, capture_output=True, check=True).stdout
    with tarfile.open(fileobj=io.BytesIO(data)) as tar:
        tar.extractall(dest, filter="data") if sys.version_info >= (3, 12) else tar.extractall(dest)  # noqa: S202
    return dest


def cr_values(tree: str) -> dict:
    """The MasIamStack spec a full install (ldap,keycloak) applies."""
    with open(os.path.join(tree, SAMPLE)) as fh:
        docs = [d for d in yaml.safe_load_all(fh) if d]
    specs = [d.get("spec") or {} for d in docs if d.get("kind") == "MasIamStack"]
    if len(specs) != 1:
        raise RuntimeError(f"{SAMPLE}: expected one MasIamStack, found {len(specs)}")
    spec = copy.deepcopy(specs[0])
    # Mirror scripts/install-olm-sample.sh's awk injections.
    sub(spec, "keycloak")["enabled"] = True
    pg = sub(spec, "postgresql")
    pg["enabled"] = True
    sub(sub(pg, "primary"), "persistence")["storageClass"] = STORAGE_CLASS
    sub(sub(spec, "openldap"), "persistence")["storageClass"] = STORAGE_CLASS
    return spec


def sub(d: dict, key: str) -> dict:
    """d[key] as a dict, creating it (a key holding only comments is None)."""
    if not isinstance(d.get(key), dict):
        d[key] = {}
    return d[key]


def render(chart_tree: str, values: dict) -> dict[tuple[str, str], dict]:
    with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as fh:
        yaml.safe_dump(values, fh)
        values_path = fh.name
    try:
        cmd = ["helm", "template", RELEASE, os.path.join(chart_tree, CHART), "--namespace", NAMESPACE, "-f", values_path]
        for api in API_VERSIONS:
            cmd += ["--api-versions", api]
        out = run(cmd)
    finally:
        os.unlink(values_path)
    objs: dict[tuple[str, str], dict] = {}
    for doc in yaml.safe_load_all(out):
        if not doc or "kind" not in doc:
            continue
        meta = doc.get("metadata") or {}
        if "helm.sh/hook" in (meta.get("annotations") or {}):
            continue
        objs[(doc["kind"], meta.get("name", ""))] = doc
    return objs


def g(obj: dict, *path, default=None):
    for p in path:
        if not isinstance(obj, dict) or p not in obj:
            return default
        obj = obj[p]
    return obj


def quantity(q) -> float | None:
    if q is None:
        return None
    s = str(q)
    units = {"Ki": 2**10, "Mi": 2**20, "Gi": 2**30, "Ti": 2**40, "K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12}
    for u, m in units.items():
        if s.endswith(u):
            return float(s[: -len(u)]) * m
    try:
        return float(s)
    except ValueError:
        return None


def immutable_changes(kind: str, old: dict, new: dict) -> list[str]:
    checks: list[tuple[str, tuple]] = []
    if kind in ("Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"):
        checks.append(("spec.selector", ("spec", "selector")))
    if kind == "StatefulSet":
        checks += [
            ("spec.serviceName", ("spec", "serviceName")),
            ("spec.podManagementPolicy", ("spec", "podManagementPolicy")),
            ("spec.volumeClaimTemplates", ("spec", "volumeClaimTemplates")),
        ]
    if kind == "Job":
        checks += [("spec.selector", ("spec", "selector")), ("spec.template", ("spec", "template"))]
    if kind == "PersistentVolumeClaim":
        checks += [
            ("spec.storageClassName", ("spec", "storageClassName")),
            ("spec.accessModes", ("spec", "accessModes")),
            ("spec.volumeMode", ("spec", "volumeMode")),
        ]
    if kind in ("RoleBinding", "ClusterRoleBinding"):
        checks.append(("roleRef", ("roleRef",)))
    if kind == "Secret":
        checks.append(("type", ("type",)))
    if kind == "Service":
        checks.append(("spec.clusterIP", ("spec", "clusterIP")))
    problems = [f"{label} changed: {g(old, *path)!r} -> {g(new, *path)!r}"
                for label, path in checks if g(old, *path) != g(new, *path)]
    if kind == "PersistentVolumeClaim":
        before = quantity(g(old, "spec", "resources", "requests", "storage"))
        after = quantity(g(new, "spec", "resources", "requests", "storage"))
        if before and after and after < before:
            problems.append(f"storage request shrank: {before:.0f} -> {after:.0f} bytes (PVCs only grow)")
    if kind in ("Secret", "ConfigMap") and old.get("immutable"):
        for field in ("data", "stringData", "binaryData"):
            if old.get(field) != new.get(field):
                problems.append(f"{field} changed on an immutable {kind}")
    return problems


def compare(label: str, old: dict, new: dict) -> tuple[list[str], list[str]]:
    errors, warns = [], []
    for key, obj in sorted(old.items()):
        kind, name = key
        if key not in new:
            keep = g(obj, "metadata", "annotations", "helm.sh/resource-policy") == "keep"
            if keep:
                continue
            msg = f"{label}: {kind}/{name} is no longer rendered; helm upgrade deletes it"
            (errors if kind in STATEFUL_KINDS else warns).append(msg)
            continue
        for p in immutable_changes(kind, obj, new[key]):
            errors.append(f"{label}: {kind}/{name}: {p}")
    return errors, warns


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--base", help="git ref of the release to upgrade from (default: last vX.Y.Z tag)")
    args = ap.parse_args()
    base = args.base or last_release_tag()

    with tempfile.TemporaryDirectory() as tmp:
        base_tree = extract(base, os.path.join(tmp, "base"))
        base_values, head_values = cr_values(base_tree), cr_values(ROOT)
        base_objs = render(base_tree, base_values)
        transitions = [
            ("operator upgraded, CR unchanged", render(ROOT, base_values)),
            ("full upgrade", render(ROOT, head_values)),
        ]

    print(f"check-chart-upgrade: {base} -> working tree ({len(base_objs)} non-hook objects in {base})")
    errors: list[str] = []
    warns: list[str] = []
    for label, objs in transitions:
        e, w = compare(label, base_objs, objs)
        errors += e
        warns += w
    for w in dict.fromkeys(warns):
        print(f"WARN  {w}")
    for e in dict.fromkeys(errors):
        print(f"ERROR {e}")
    if errors:
        print(f"check-chart-upgrade: {len(errors)} error(s): upgrading an install from {base} would fail or lose data")
        return 1
    print(f"check-chart-upgrade: ok ({len(warns)} warning(s))")
    return 0


if __name__ == "__main__":
    sys.exit(main())
