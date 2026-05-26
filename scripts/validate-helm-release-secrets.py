#!/usr/bin/env python3
# validate-helm-release-secrets.py — Custom FR-V4-37 enforcement (US-V4-09).
#
# Asserts no `helm_release` block in terraform/*.tf passes a sensitive Terraform
# variable through `set { name = X, value = var.<sensitive_var> }`. The
# rendered chart MUST instead consume the secret via an ExternalSecret managed
# under gitops/ (mode 1 / mode 2 in terraform/locals.tf).
#
# Implemented as a Python validator (rather than a Go-built tflint plugin) per
# the codebase convention — see scripts/validate-action-pins.py,
# validate-cluster-registry.py, validate-workflow-call-contract.py. tflint
# plugins require a `go build` step that this repo's CI does not currently
# provision; the in-tree Python validators are pinned via `.tool-versions` and
# wired into both pre-commit and CI in lockstep with the other gates.
#
# Detection rules
# ───────────────
# Fails the PR if a `helm_release` block contains ANY of:
#   1. `set { name = "..." value = var.<sensitive> }` — strict FR-V4-37 wording
#      (var.<X> where X is declared `sensitive = true` in variables.tf).
#   2. `set { name = "..." value = local.<X> }` where local.<X> reduces to
#      `var.<sensitive_var>` (single-hop). Caught because the same leak risk
#      exists when a one-line `local` aliases a sensitive var.
#   3. `set { name = "..." value = <resource>.<X>.<attr> }` where the attr is
#      a known-sensitive attribute (administrator_password, value on
#      azuread_service_principal_password, data.token on a Kubernetes secret).
#
# Allowed shapes
# ──────────────
#   - Non-sensitive references (e.g. fqdn, client_id, names) — always fine.
#   - `set { name = "envFrom[N].secretRef.name", value = "<secret-name>" }` —
#     this is the canonical migration target. The value is the K8s Secret NAME
#     (not its contents) and the Secret is materialised by ESO from AKV.
#
# Output
# ──────
#   - Exits 0 on clean.
#   - Exits 1 with GitHub Actions `::error::` annotations on the first
#     violation. Each annotation points at the file, the resource label,
#     and the offending `set` line, with a pointer to FR-V4-37.

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
TERRAFORM_DIR = REPO_ROOT / "terraform"
VARIABLES_FILE = TERRAFORM_DIR / "variables.tf"
LOCALS_FILE = TERRAFORM_DIR / "locals.tf"
HELP_URL = "PRD-v4 FR-V4-37 / US-V4-09 — migrate via AKV + ExternalSecret (see terraform/locals.tf)."

# Match: resource "helm_release" "<label>" { ... }
HELM_RELEASE_RE = re.compile(
    r'resource\s+"helm_release"\s+"(?P<label>[^"]+)"\s*\{(?P<body>.*?)\n\}',
    re.DOTALL,
)

# Match a single `set { ... }` block inside a helm_release body.
SET_BLOCK_RE = re.compile(
    r'(?P<indent>^[ \t]*)set\s*\{(?P<body>[^{}]*)\}',
    re.MULTILINE,
)

# Within a set block: `name = "<X>"` and `value = <expr>` (with the trailing
# token captured greedily up to newline).
NAME_RE = re.compile(r'\bname\s*=\s*"([^"]+)"')
VALUE_RE = re.compile(r'\bvalue\s*=\s*([^\n]+)')

# `var.<NAME>`, optionally chained with attributes.
VAR_REF_RE = re.compile(r'\bvar\.([A-Za-z0-9_]+)\b')
LOCAL_REF_RE = re.compile(r'\blocal\.([A-Za-z0-9_]+)\b')

# Sensitive attribute names that should never flow through helm_release.set.
# Curated rather than heuristic so the validator stays deterministic — a new
# sensitive attribute requires an explicit entry here.
KNOWN_SENSITIVE_ATTRS = {
    # azurerm_postgresql_flexible_server.administrator_password
    "administrator_password",
    # azuread_service_principal_password.value
    "azuread_service_principal_password",
    # kubernetes_secret of type service-account-token: data.token
    "service_account_secret",
}

# Patterns that match a sensitive resource access. The key is a short tag used
# in the error message; the value is the regex applied to the `value = ...`
# right-hand side.
SENSITIVE_RESOURCE_PATTERNS = [
    (
        "azurerm_postgresql_flexible_server administrator_password",
        re.compile(r'azurerm_postgresql_flexible_server\b[^\n]*\.administrator_password\b'),
    ),
    (
        "azuread_service_principal_password value",
        re.compile(r'azuread_service_principal_password\b[^\n]*\.value\b'),
    ),
    (
        "kubernetes_secret service-account-token data.token",
        re.compile(r'kubernetes_secret\.[A-Za-z0-9_]*service_account[A-Za-z0-9_]*\b[^\n]*\.data\.token\b'),
    ),
]

# Regex for a name-attr that is the canonical migration target — k8s Secret
# name passed via `envFrom[N].secretRef.name`. The value here is the Secret's
# *name*, not its contents, so it is never a finding.
ENVFROM_SECRET_REF_RE = re.compile(r'^envFrom\[\d+\]\.secretRef\.name$')


def _annotate(file: Path, line: int, message: str) -> None:
    try:
        rel = file.relative_to(REPO_ROOT) if file.is_absolute() else file
    except ValueError:
        rel = file
    print(f"::error file={rel},line={line}::{message}")


def parse_sensitive_variables(text: str) -> set[str]:
    """Return the set of variable names declared with `sensitive = true`."""
    sensitive: set[str] = set()
    # Match `variable "<NAME>" { ... sensitive = true ... }` blocks.
    block_re = re.compile(
        r'variable\s+"([A-Za-z0-9_]+)"\s*\{((?:[^{}]|\{[^{}]*\})*)\}',
        re.DOTALL,
    )
    for m in block_re.finditer(text):
        name = m.group(1)
        body = m.group(2)
        if re.search(r'\bsensitive\s*=\s*true\b', body):
            sensitive.add(name)
    return sensitive


def parse_local_aliases(text: str) -> dict[str, str]:
    """Return {local_name: right-hand-side-text} for top-level locals.

    Only single-line `name = <expr>` assignments are recognised — that's the
    only shape we currently care about (one-hop `local.X = var.<sensitive>`
    aliases). Multi-line/object locals are skipped.
    """
    aliases: dict[str, str] = {}
    in_locals = False
    depth = 0
    for line in text.splitlines():
        stripped = line.strip()
        if not in_locals:
            if re.match(r'^locals\s*\{', stripped):
                in_locals = True
                depth = stripped.count("{") - stripped.count("}")
            continue
        depth += stripped.count("{") - stripped.count("}")
        if depth <= 0:
            in_locals = False
            continue
        # Top-level locals only (depth == 1 after entering).
        if depth != 1:
            continue
        m = re.match(r'^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?)(?:\s*#.*)?$', stripped)
        if m:
            aliases[m.group(1)] = m.group(2).strip()
    return aliases


def value_is_forbidden(
    value_expr: str,
    sensitive_vars: set[str],
    local_aliases: dict[str, str],
) -> str | None:
    """Return a short human-readable reason if `value_expr` is forbidden, else None."""
    # Rule 1: direct var.<sensitive>
    for m in VAR_REF_RE.finditer(value_expr):
        if m.group(1) in sensitive_vars:
            return f"references sensitive variable var.{m.group(1)}"
    # Rule 2: local.<X> where local.X = var.<sensitive>
    for m in LOCAL_REF_RE.finditer(value_expr):
        alias_target = local_aliases.get(m.group(1), "")
        for vm in VAR_REF_RE.finditer(alias_target):
            if vm.group(1) in sensitive_vars:
                return (
                    f"references local.{m.group(1)} which aliases sensitive "
                    f"variable var.{vm.group(1)}"
                )
    # Rule 3: known-sensitive resource attribute
    for tag, pat in SENSITIVE_RESOURCE_PATTERNS:
        if pat.search(value_expr):
            return f"references {tag}"
    return None


def find_violations(tf_dir: Path) -> list[tuple[Path, int, str, str, str]]:
    """Return (file, line_no, resource_label, set_name, reason) tuples."""
    if not VARIABLES_FILE.exists():
        return []
    sensitive_vars = parse_sensitive_variables(VARIABLES_FILE.read_text(encoding="utf-8"))

    # Pool all `locals { ... }` blocks across the terraform/ tree (main.tf,
    # keyvaults.tf, locals.tf, …). One-hop alias detection is all we cover.
    local_aliases: dict[str, str] = {}
    for tf in sorted(tf_dir.glob("*.tf")):
        local_aliases.update(parse_local_aliases(tf.read_text(encoding="utf-8")))

    findings: list[tuple[Path, int, str, str, str]] = []
    for tf in sorted(tf_dir.glob("*.tf")):
        text = tf.read_text(encoding="utf-8")
        # Strip line-comments so commented examples in catalogue/docs don't
        # match — same convention as validate-akv-catalogue / null-expiry.
        text_no_comments = re.sub(r"#[^\n]*", "", text)
        for hr in HELM_RELEASE_RE.finditer(text_no_comments):
            label = hr.group("label")
            body = hr.group("body")
            body_start_offset = hr.start("body")
            for sm in SET_BLOCK_RE.finditer(body):
                set_body = sm.group("body")
                name_m = NAME_RE.search(set_body)
                value_m = VALUE_RE.search(set_body)
                if not value_m:
                    continue
                set_name = name_m.group(1) if name_m else "<unknown>"
                # Allowed shape: envFrom[N].secretRef.name (migration target).
                if name_m and ENVFROM_SECRET_REF_RE.match(set_name):
                    continue
                value_expr = value_m.group(1).strip()
                reason = value_is_forbidden(
                    value_expr, sensitive_vars, local_aliases
                )
                if reason is None:
                    continue
                # Compute 1-indexed line number of the offending `set` block
                # in the ORIGINAL source. The offset returned by SET_BLOCK_RE
                # is relative to the helm_release body; add body_start_offset
                # and count newlines up to that point in the de-commented text
                # (line numbering matches the original because we replaced
                # comments in place with empty strings via re.sub).
                abs_offset = body_start_offset + sm.start()
                line_no = text_no_comments[:abs_offset].count("\n") + 1
                findings.append((tf, line_no, label, set_name, reason))
    return findings


def main() -> int:
    findings = find_violations(TERRAFORM_DIR)
    if not findings:
        print(
            "[PASS] No helm_release.set { value = sensitive } shortcut found "
            "in terraform/*.tf (FR-V4-37 satisfied)."
        )
        return 0
    print(f"[FAIL] {len(findings)} helm_release.set sensitivity violation(s):", file=sys.stderr)
    for tf, line, label, set_name, reason in findings:
        try:
            rel = tf.relative_to(REPO_ROOT)
        except ValueError:
            rel = tf
        msg = (
            f'helm_release."{label}" set {{ name = "{set_name}" }} {reason}. '
            f"Migrate via AKV + ExternalSecret. {HELP_URL}"
        )
        _annotate(tf, line, msg)
        print(f"  {rel}:{line}  helm_release.{label}  name={set_name}  -- {reason}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
