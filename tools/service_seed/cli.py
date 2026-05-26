"""Cluster topology registry loader for service_seed (FR-V4-03, ADR-031-v4).

This module is the *only* path through which service_seed reads per-cluster
identity (subscription_id, region, resource_group, acr_hostname, aks_name,
mgmt_role). seed_job.py (and its v4 replacements: jira_intake.py,
service_template.py, gitops_pr.py per US-V4-07) must import from here.

The registry file is committed at ``gitops/clusters/registry.yaml`` and is
schema-validated by ``scripts/validate-cluster-registry.py`` in pre-commit and
CI; loaders here do *not* re-validate (single ownership of validation per
FR-V4-04).

Public surface:
    load_registry(path=None) -> dict[str, ClusterEntry]
        Returns a mapping keyed by cluster aks_name. Path defaults to the
        repo-relative gitops/clusters/registry.yaml.

    get_cluster(name, registry=None) -> ClusterEntry
        Convenience: load + key lookup with a typed exception on miss.

    workload_keyvault_id(cluster_name, vault_name, registry=None) -> str
        Build the canonical ARM resource ID for a workload AKV given the
        cluster's subscription and resource_group. Centralizes the format
        previously hardcoded in seed_job.py.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any


_REPO_ROOT = Path(__file__).resolve().parents[2]
_DEFAULT_REGISTRY_PATH = _REPO_ROOT / "gitops" / "clusters" / "registry.yaml"


class ClusterRegistryError(Exception):
    """Raised when the cluster registry is missing, unreadable, or malformed."""


@dataclass(frozen=True)
class ClusterEntry:
    name: str
    subscription_id: str
    region: str
    region_abbrev: str
    resource_group: str
    acr_hostname: str
    aks_name: str
    mgmt_role: str
    azs: tuple[str, ...]
    sku_tier: str
    gitops_addons: dict[str, str]


def _yaml_load(text: str) -> dict[str, Any]:
    """Load registry YAML. Prefers PyYAML; falls back to a tiny safe parser
    so the loader works in environments where pyyaml is not installed
    (e.g., minimal CI runners).
    """
    try:
        import yaml  # type: ignore[import-untyped]

        loaded = yaml.safe_load(text)
        if not isinstance(loaded, dict):
            raise ClusterRegistryError("registry root must be a mapping")
        return loaded
    except ImportError:
        return _parse_registry_fallback(text)


def _parse_registry_fallback(text: str) -> dict[str, Any]:
    """Minimal YAML subset parser for the registry shape.

    Handles 2-space indented mappings, scalar values, and ``- "x"`` list items.
    Comments and blank lines are skipped. Quotes (single or double) are
    stripped. Sufficient for gitops/clusters/registry.yaml; not a general YAML.

    Container shape is decided when the *first* child of a pending key is seen:
    a list-item child makes it a list, a key:value child makes it a dict.
    """
    result: dict[str, Any] = {}
    # Each frame is (indent, container, pending_key). When pending_key is set,
    # the next deeper line decides whether container[pending_key] becomes a
    # dict (key:value child) or a list (- item child).
    stack: list[list[Any]] = [[-1, result, None]]

    for raw_line in text.splitlines():
        line = raw_line.split("#", 1)[0].rstrip()
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip(" "))
        body = line.lstrip(" ")

        # Unwind frames whose indent is no longer in scope. A pending_key with
        # no children resolves to an empty dict (keeps simple parse semantics).
        while stack and indent <= stack[-1][0]:
            frame_indent, container, pending = stack[-1]
            if pending is not None and pending not in (container or {}):
                container[pending] = {}
            stack.pop()
        if not stack:
            raise ClusterRegistryError(f"indent underflow at: {raw_line!r}")

        frame = stack[-1]
        _, container, pending_key = frame

        if body.startswith("- "):
            item = _strip_scalar(body[2:].strip())
            if pending_key is not None:
                # First child of pending key is a list item → materialize as list.
                if pending_key not in container:
                    container[pending_key] = []
                target = container[pending_key]
                if not isinstance(target, list):
                    raise ClusterRegistryError(f"list under non-list key: {pending_key}")
                target.append(item)
            elif isinstance(container, list):
                container.append(item)
            else:
                raise ClusterRegistryError(f"unexpected list item: {raw_line!r}")
            continue

        if ":" not in body:
            raise ClusterRegistryError(f"malformed line: {raw_line!r}")
        key, _, rest = body.partition(":")
        key = key.strip()
        rest = rest.strip()

        if pending_key is not None:
            # First child of pending key is a mapping line → materialize as dict.
            new_container: dict[str, Any] = {}
            container[pending_key] = new_container
            stack[-1][1] = new_container
            stack[-1][2] = None
            container = new_container

        if rest == "":
            # Push a deeper frame carrying the pending key. The outer frame's
            # pending state is cleared so subsequent siblings of `key` do not
            # see stale pending state when unwinding.
            stack[-1][2] = None
            stack.append([indent, container, key])
        elif rest == "[]":
            container[key] = []
        else:
            container[key] = _strip_scalar(rest)

    # Resolve any trailing pending keys to empty dicts.
    for indent_v, container, pending in stack:
        if pending is not None and pending not in (container or {}):
            container[pending] = {}

    return result


def _strip_scalar(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
        return value[1:-1]
    return value


def load_registry(path: Path | str | None = None) -> dict[str, ClusterEntry]:
    """Load and parse the cluster topology registry.

    Args:
        path: Path to registry YAML. Defaults to repo-relative gitops/clusters/registry.yaml.

    Returns:
        Mapping {cluster_name: ClusterEntry}. Ordered by registry insertion order.

    Raises:
        ClusterRegistryError: if the file is missing, unreadable, or rejects the typed-load.
    """
    resolved = Path(path) if path is not None else _DEFAULT_REGISTRY_PATH
    if not resolved.is_file():
        raise ClusterRegistryError(f"registry not found: {resolved}")
    try:
        raw_text = resolved.read_text(encoding="utf-8")
    except OSError as exc:
        raise ClusterRegistryError(f"cannot read registry {resolved}: {exc}") from exc

    raw = _yaml_load(raw_text)
    return {
        name: _build_entry(name, entry) for name, entry in raw.items()
    }


def _build_entry(name: str, entry: Any) -> ClusterEntry:
    if not isinstance(entry, dict):
        raise ClusterRegistryError(f"entry {name} is not a mapping")
    required = (
        "subscription_id", "region", "region_abbrev", "resource_group",
        "acr_hostname", "aks_name", "mgmt_role", "azs", "sku_tier",
        "gitops_addons",
    )
    missing = [f for f in required if f not in entry]
    if missing:
        raise ClusterRegistryError(f"entry {name} missing required field(s): {missing}")
    if entry["aks_name"] != name:
        raise ClusterRegistryError(
            f"entry {name}: aks_name ({entry['aks_name']!r}) must equal top-level key"
        )
    azs_raw = entry["azs"]
    if not isinstance(azs_raw, list):
        raise ClusterRegistryError(f"entry {name}: azs must be a list")
    addons = entry["gitops_addons"]
    if not isinstance(addons, dict):
        raise ClusterRegistryError(f"entry {name}: gitops_addons must be a mapping")
    return ClusterEntry(
        name=name,
        subscription_id=str(entry["subscription_id"]),
        region=str(entry["region"]),
        region_abbrev=str(entry["region_abbrev"]),
        resource_group=str(entry["resource_group"]),
        acr_hostname=str(entry["acr_hostname"]),
        aks_name=str(entry["aks_name"]),
        mgmt_role=str(entry["mgmt_role"]),
        azs=tuple(str(z) for z in azs_raw),
        sku_tier=str(entry["sku_tier"]),
        gitops_addons={str(k): str(v) for k, v in addons.items()},
    )


def get_cluster(name: str, registry: dict[str, ClusterEntry] | None = None) -> ClusterEntry:
    """Return the cluster entry for ``name`` or raise ClusterRegistryError."""
    reg = registry if registry is not None else load_registry()
    if name not in reg:
        raise ClusterRegistryError(
            f"cluster {name!r} not found in registry; known: {sorted(reg)}"
        )
    return reg[name]


def workload_keyvault_id(
    cluster_name: str,
    vault_name: str,
    registry: dict[str, ClusterEntry] | None = None,
) -> str:
    """Build the canonical ARM resource ID for an AKV in ``cluster_name``'s RG.

    Replaces the hardcoded ``/subscriptions/.../vaults/kv-platform-prod-{we,ne}``
    string that lived inside seed_job.py before FR-V4-03.
    """
    entry = get_cluster(cluster_name, registry=registry)
    return (
        f"/subscriptions/{entry.subscription_id}"
        f"/resourceGroups/{entry.resource_group}"
        f"/providers/Microsoft.KeyVault/vaults/{vault_name}"
    )


def main(argv: list[str] | None = None) -> int:
    """CLI entry point: ``service-seed-registry [show|paths] [name]``.

    Used by operators to verify what cli.py sees without invoking seed_job.
    """
    import argparse
    import json

    parser = argparse.ArgumentParser(prog="service-seed-registry")
    parser.add_argument("command", choices=("show", "paths"), help="show=dump entries; paths=registry file location")
    parser.add_argument("name", nargs="?", help="optional cluster name to filter on")
    parser.add_argument("--registry", help="override registry path")
    args = parser.parse_args(argv)

    if args.command == "paths":
        print(args.registry or str(_DEFAULT_REGISTRY_PATH))
        return 0

    reg = load_registry(args.registry)
    if args.name:
        if args.name not in reg:
            parser.error(f"cluster {args.name!r} not in registry; known: {sorted(reg)}")
        entries: dict[str, Any] = {args.name: _entry_to_dict(reg[args.name])}
    else:
        entries = {n: _entry_to_dict(e) for n, e in reg.items()}
    print(json.dumps(entries, indent=2, sort_keys=True))
    return 0


def _entry_to_dict(entry: ClusterEntry) -> dict[str, Any]:
    return {
        "subscription_id": entry.subscription_id,
        "region": entry.region,
        "region_abbrev": entry.region_abbrev,
        "resource_group": entry.resource_group,
        "acr_hostname": entry.acr_hostname,
        "aks_name": entry.aks_name,
        "mgmt_role": entry.mgmt_role,
        "azs": list(entry.azs),
        "sku_tier": entry.sku_tier,
        "gitops_addons": entry.gitops_addons,
    }


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())
