# .tflint.hcl — tflint configuration (US-V3-11, US-V3-16, ADR-025-v3, ADR-026-v3)
#
# azurerm ruleset enforces naming, SKU, and tag conventions.
# Additional custom rules are proposed via ADR before being added.

config {
  call_module_type = "local"
}

plugin "azurerm" {
  enabled = true
  version = "0.26.0"
  source  = "github.com/terraform-linters/tflint-ruleset-azurerm"
}
