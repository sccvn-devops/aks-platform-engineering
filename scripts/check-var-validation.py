#!/usr/bin/env python3
"""
check-var-validation.py — custom tflint companion rule (US-V3-16, ADR-026-v3)

Detects Terraform `variable` declarations that lack a `validation {}` block.
Emits findings in a format compatible with CI annotations.

Gate phase (ADR-026-v3 decision #3):
  Ratchet mode (default): exits 0 — findings are warnings, CI advisory only.
  Blocking mode (--block): exits 1 if any unvalidated variables are found.

BLOCKING CUTOVER CHECKLIST:
  1. Ensure all variables in terraform/ have validation {} blocks (or are allow-listed).
  2. Change soft-fail gate in CI: remove `|| true` from check-var-validation step.
  3. Add --block flag to the script invocation in .github/workflows/terraform-ci.yml.

Usage:
  python3 scripts/check-var-validation.py [--dir terraform] [--block] [--allow-list vars.txt]

Allow-list file format (one variable name per line, with optional rationale comment):
  github_token          # optional: irrelevant to IaC provisioning, no meaningful constraints
  addons                # optional: type=any, validation not expressible without schema
"""

import re
import sys
import glob
import argparse
from pathlib import Path

ALLOW_LIST_DEFAULT = {
    # Variables whose type=any makes inline validation impractical.
    "addons",
    "addons_versions",
    "green_field_application_gateway_for_ingress",
    # Variables that are boolean flags — validation {} would only duplicate the type constraint.
    "role_based_access_control_enabled",
    "rbac_aad",
    "private_cluster_enabled",
    "enable_auto_scaling",
    "enable_host_encryption",
    "log_analytics_workspace_enabled",
    "azure_policy_enabled",
    "microsoft_defender_enabled",
    "create_role_assignments_for_application_gateway",
    "build_backstage",
    # Variables whose values are inherently free-form or module-internal.
    "github_token",
    "git_private_ssh_key",
    "git_public_ssh_key",
    "gitops_addons_org",
    "gitops_addons_repo",
    "gitops_addons_revision",
    "gitops_addons_repo_url",
    "gitops_addons_basepath",
    "gitops_addons_path",
    "tags",
    "kubernetes_version",
    "os_sku",
    "os_disk_size_gb",
    "agents_size",
    "agents_min_count",
    "agents_max_count",
    "agents_max_pods",
    # Sensitive variables — shape validated at sourcing layer (AKV / OIDC)
    "postgres_password",
    "jenkins_admin_password",
    "jenkins_bitbucket_workspace_token",
    "jenkins_jira_service_account_token",
    "jenkins_webhook_https_keystore_base64",
    "jenkins_webhook_https_keystore_password",
    # Free-form URL/string vars whose format varies per operator
    "jira_base_url",
    "jira_project_key",
    "jira_service_account_email",
    "jenkins_bitbucket_server_url",
    "jenkins_bitbucket_repo_owner",
    "jenkins_service_repository",
    "jenkins_platform_gitops_repo_url",
    "jenkins_admin_username",
    "resource_group_name",
    "jenkins_webhook_allowed_ipv4_cidrs",
    # Numeric/boolean — validation {} would duplicate the type constraint
    "agents_min_count",
    "agents_max_count",
    "agents_max_pods",
    "os_disk_size_gb",
}


def parse_variables(hcl_content: str) -> list[dict]:
    """
    Extract variable blocks from HCL content using a simple bracket-depth parser.
    Returns a list of dicts: {name, has_validation, line}.
    """
    variables = []
    lines = hcl_content.splitlines()
    i = 0
    while i < len(lines):
        line = lines[i].strip()
        m = re.match(r'^variable\s+"([^"]+)"\s*\{', line)
        if m:
            var_name = m.group(1)
            var_line = i + 1  # 1-indexed
            depth = line.count("{") - line.count("}")
            has_validation = "validation" in line
            j = i + 1
            while j < len(lines) and depth > 0:
                cur = lines[j].strip()
                depth += cur.count("{") - cur.count("}")
                if re.match(r"^validation\s*\{", cur):
                    has_validation = True
                j += 1
            variables.append({
                "name": var_name,
                "has_validation": has_validation,
                "line": var_line,
            })
            i = j
        else:
            i += 1
    return variables


def main() -> int:
    parser = argparse.ArgumentParser(description="Check Terraform variables for validation {} blocks")
    parser.add_argument("--dir", default="terraform", help="Terraform directory to scan")
    parser.add_argument("--block", action="store_true",
                        help="Exit 1 if unvalidated variables are found (blocking mode)")
    parser.add_argument("--allow-list", metavar="FILE",
                        help="File with additional allow-listed variable names (one per line)")
    args = parser.parse_args()

    allow_list = set(ALLOW_LIST_DEFAULT)
    if args.allow_list:
        try:
            with open(args.allow_list) as f:
                for ln in f:
                    name = ln.split("#")[0].strip()
                    if name:
                        allow_list.add(name)
        except FileNotFoundError:
            print(f"WARNING: allow-list file '{args.allow_list}' not found; proceeding without it.")

    tf_files = sorted(glob.glob(f"{args.dir}/**/*.tf", recursive=True))
    if not tf_files:
        print(f"No .tf files found in '{args.dir}'")
        return 0

    findings = []
    total_vars = 0

    for tf_file in tf_files:
        # Skip .terraform/ module cache
        if ".terraform/" in tf_file or "/.terraform" in tf_file:
            continue
        try:
            content = Path(tf_file).read_text()
        except IOError as e:
            print(f"WARNING: cannot read {tf_file}: {e}")
            continue

        for var in parse_variables(content):
            total_vars += 1
            if not var["has_validation"] and var["name"] not in allow_list:
                findings.append({
                    "file": tf_file,
                    "line": var["line"],
                    "name": var["name"],
                })

    mode_label = "error" if args.block else "warning"

    if findings:
        print(f"\n[check-var-validation] {mode_label.upper()}: {len(findings)} variable(s) missing validation {{}} block\n")
        for f in findings:
            # GitHub Actions annotation format
            print(f"::{mode_label} file={f['file']},line={f['line']}::"
                  f"variable \"{f['name']}\" has no validation {{}} block "
                  f"(ADR-026-v3; add validation or add to allow-list)")
        print(f"\n  Total variables scanned: {total_vars}")
        print(f"  Unvalidated (not allow-listed): {len(findings)}")
        print(f"  Coverage: {((total_vars - len(findings)) / total_vars * 100):.0f}%")
        if not args.block:
            print("\n  Running in advisory mode. To block on failures: add --block flag.")
        return 1 if args.block else 0

    print(f"[check-var-validation] OK: all {total_vars} variable(s) have validation {{}} blocks or are allow-listed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
