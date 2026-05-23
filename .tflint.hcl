# .tflint.hcl — tflint configuration (US-V3-11, US-V3-16, ADR-025-v3, ADR-026-v3)
#
# tflint-ruleset-azurerm enforces cloud-resource-level rules (naming, SKU, tags).
# Inline validation {} in variables.tf covers input shape (ADR-026-v3 decision #2 split).
#
# Companion script: scripts/check-var-validation.py runs alongside tflint in CI to
# detect variables missing validation {} blocks (advisory during ratchet; see CI workflow).
#
# Rule overrides are documented inline.  Adding or disabling a rule requires
# an ADR entry per ADR-025-v3.

config {
  call_module_type = "local"
}

plugin "azurerm" {
  enabled = true
  version = "0.26.0"
  source  = "github.com/terraform-linters/tflint-ruleset-azurerm"
}

# ---------------------------------------------------------------------------
# Rule overrides — document every disable with its rationale.
#
# To disable a rule:
#   rule "<rule_name>" {
#     enabled = false
#     # Reason: <rationale>
#     # ADR: <ADR reference if a formal decision was made>
#   }
#
# Current overrides (none at day-1 GA):
#   All azurerm ruleset rules are enabled at their default severity.
#   Override examples are documented here for team reference:
#
#   rule "azurerm_resource_group_invalid_location" {
#     enabled = false
#     # Reason: location list is maintained by platform team; tflint list may lag behind
#     #         Azure's actual supported regions for new SKUs.
#     # ADR: ADR-026-v3 (review at sprint-2 if false-positives surface)
#   }
# ---------------------------------------------------------------------------
