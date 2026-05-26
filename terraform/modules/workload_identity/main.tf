# ─────────────────────────────────────────────────────────────────────────────
# Workload Identity module (US-V4-02 / FR-V4-05..09)
#
# Single locality for the UAMI + federated-credential + role-assignment triple
# that every platform workload identity needs.  Adding a new workload identity
# is now ~10 lines at the call site instead of ~30 lines duplicated across
# three separate TF resources.
#
# The federated-credential subject is constructed inside the module so call
# sites cannot accidentally drift on the subject format.  The subscription-
# scope guard (allow_subscription_scope = false by default) forces a deliberate
# opt-in for cross-RG / cross-subscription role grants — the
# "least-privilege-by-default" stance defended in the v4 grilling session.
#
# Inputs/outputs/test contracts: see variables.tf, outputs.tf, tests/.
# ─────────────────────────────────────────────────────────────────────────────

resource "azurerm_user_assigned_identity" "this" {
  name                = var.name
  location            = var.location
  resource_group_name = var.resource_group_name
  tags                = var.tags
}

resource "azurerm_federated_identity_credential" "this" {
  for_each = var.federated_credentials

  name                = coalesce(each.value.name, each.key)
  resource_group_name = var.resource_group_name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = each.value.issuer
  parent_id           = azurerm_user_assigned_identity.this.id
  subject             = "system:serviceaccount:${each.value.service_account_namespace}:${each.value.service_account_name}"
}

resource "azurerm_role_assignment" "this" {
  for_each = var.role_assignments

  scope                = each.value.scope
  role_definition_name = each.value.role_definition_name
  principal_id         = azurerm_user_assigned_identity.this.principal_id

  lifecycle {
    precondition {
      # FR-V4-08: refuse subscription-scoped role grants unless explicitly allowed.
      # A subscription-scoped grant is any scope matching /subscriptions/<uuid>
      # with no further path segments. /subscriptions/<uuid>/resourceGroups/...
      # passes (RG-scope is fine by default).
      condition     = var.allow_subscription_scope || !can(regex("^/subscriptions/[0-9a-fA-F-]{36}$", each.value.scope))
      error_message = "workload_identity[${var.name}].role_assignments[${each.key}] is subscription-scoped (${each.value.scope}); set allow_subscription_scope = true to authorize."
    }
  }
}
