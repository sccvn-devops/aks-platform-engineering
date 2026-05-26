#!/usr/bin/env python3
"""Cluster topology registry schema validator (FR-V4-04, ADR-031-v4).

Runs in pre-commit and in CI. Returns 0 if every entry in
gitops/clusters/registry.yaml conforms to gitops/clusters/registry.schema.json;
returns 1 otherwise. Emits GitHub Actions ``::error file=...,line=...`` annotations
on failure so PR comments point at the offending entry.

Two backends, tried in this order:
  1. jsonschema (preferred — most accurate error messages).
  2. Embedded minimal validator (handles every keyword used by the registry
     schema: required, additionalProperties, type, pattern, enum,
     minProperties, uniqueItems, items). Used when jsonschema isn't on PATH.

Usage:
    scripts/validate-cluster-registry.py                # default paths
    scripts/validate-cluster-registry.py --registry X --schema Y
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_REGISTRY = REPO_ROOT / "gitops" / "clusters" / "registry.yaml"
DEFAULT_SCHEMA = REPO_ROOT / "gitops" / "clusters" / "registry.schema.json"


def _load_yaml(path: Path) -> Any:
    try:
        import yaml  # type: ignore[import-untyped]

        return yaml.safe_load(path.read_text(encoding="utf-8"))
    except ImportError:
        # Reuse the loader the cli.py path uses so the validator and the
        # consumer can never diverge on what counts as a parseable registry.
        sys.path.insert(0, str(REPO_ROOT))
        from tools.service_seed.cli import _parse_registry_fallback

        return _parse_registry_fallback(path.read_text(encoding="utf-8"))


def _emit_error(path: Path, message: str) -> None:
    """Emit a GitHub Actions error annotation pointing at the registry file."""
    print(f"::error file={path}::{message}", file=sys.stderr)


class _MinimalValidator:
    """Validates the registry-schema feature subset without external deps."""

    def __init__(self, schema: dict[str, Any]) -> None:
        self.schema = schema
        self.errors: list[str] = []

    def validate(self, instance: Any, schema: dict[str, Any] | None = None, path: str = "$") -> None:
        s = schema if schema is not None else self.schema
        t = s.get("type")
        if t == "object":
            if not isinstance(instance, dict):
                self.errors.append(f"{path}: expected object")
                return
            if "minProperties" in s and len(instance) < s["minProperties"]:
                self.errors.append(f"{path}: must have at least {s['minProperties']} properties")
            required = s.get("required", [])
            for field in required:
                if field not in instance:
                    self.errors.append(f"{path}: missing required field {field!r}")
            props = s.get("properties", {})
            allow_extra = s.get("additionalProperties", True)
            for k, v in instance.items():
                if k in props:
                    self.validate(v, props[k], f"{path}.{k}")
                elif isinstance(allow_extra, dict):
                    self.validate(v, allow_extra, f"{path}.{k}")
                elif allow_extra is False:
                    self.errors.append(f"{path}: unexpected property {k!r}")
        elif t == "array":
            if not isinstance(instance, list):
                self.errors.append(f"{path}: expected array")
                return
            if s.get("uniqueItems") and len(instance) != len(set(map(_hashable, instance))):
                self.errors.append(f"{path}: items must be unique")
            items = s.get("items")
            if items is not None:
                for idx, item in enumerate(instance):
                    self.validate(item, items, f"{path}[{idx}]")
        elif t == "string":
            if not isinstance(instance, str):
                self.errors.append(f"{path}: expected string")
                return
            if "pattern" in s and not re.search(s["pattern"], instance):
                self.errors.append(f"{path}: {instance!r} does not match {s['pattern']!r}")
            if "enum" in s and instance not in s["enum"]:
                self.errors.append(f"{path}: {instance!r} not in {s['enum']}")


def _hashable(x: Any) -> Any:
    if isinstance(x, list):
        return tuple(_hashable(y) for y in x)
    if isinstance(x, dict):
        return tuple(sorted((k, _hashable(v)) for k, v in x.items()))
    return x


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--registry", default=str(DEFAULT_REGISTRY))
    parser.add_argument("--schema", default=str(DEFAULT_SCHEMA))
    args = parser.parse_args(argv)

    registry_path = Path(args.registry)
    schema_path = Path(args.schema)
    if not registry_path.is_file():
        _emit_error(registry_path, "registry file does not exist")
        return 1
    if not schema_path.is_file():
        _emit_error(schema_path, "schema file does not exist")
        return 1

    schema = json.loads(schema_path.read_text(encoding="utf-8"))
    registry = _load_yaml(registry_path)

    # Prefer jsonschema when present — better diagnostics, supports more keywords.
    try:
        import jsonschema  # type: ignore[import-not-found]

        try:
            jsonschema.validate(instance=registry, schema=schema)
            print(f"[OK] {registry_path} conforms to {schema_path}")
            return 0
        except jsonschema.ValidationError as exc:
            _emit_error(registry_path, f"schema violation at {list(exc.absolute_path)}: {exc.message}")
            return 1
    except ImportError:
        pass

    validator = _MinimalValidator(schema)
    validator.validate(registry)
    if validator.errors:
        for err in validator.errors:
            _emit_error(registry_path, err)
        return 1
    print(f"[OK] {registry_path} conforms to {schema_path} (embedded validator)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
