# workload_identity

Single locality for the UAMI + federated-credential + role-assignment triple
that every platform workload identity needs.  Closes FR-V4-05..09 / US-V4-02.

## Why

Before this module, the canonical pattern was duplicated ~6 times across
`terraform/*.tf` for Jenkins, ESO (per-workload), ESO (mgmt-we), Velero,
akv-sync-exporter, and saas-token-rotator.  Each call site:

1. Declared an `azurerm_user_assigned_identity`.
2. Declared one or more `azurerm_federated_identity_credential` resources
   with a hand-spelled `system:serviceaccount:<ns>:<sa>` subject.
3. Declared a varying number of `azurerm_role_assignment` resources.

The duplication invited subject-string typos, ad-hoc subscription-scope
grants, and a missing audit trail for "where does this UAMI hold a role?".
This module collapses the triple into a single call.

## Usage

```hcl
module "velero_identity" {
  source              = "./modules/workload_identity"
  name                = "uami-velero"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  tags                = merge(var.tags, { cluster = "mgmt-we", purpose = "backup" })

  federated_credentials = {
    "velero-server-mgmt-we" = {
      issuer                    = module.aks.oidc_issuer_url
      service_account_namespace = "velero"
      service_account_name      = "velero-server"
    }
  }

  role_assignments = {
    "storage_blob_data_contributor" = {
      scope                = azurerm_storage_account.mgmt_backup.id
      role_definition_name = "Storage Blob Data Contributor"
    }
    "rg_contributor" = {
      scope                = azurerm_resource_group.this.id
      role_definition_name = "Contributor"
    }
  }
}
```

## Inputs

| Name | Type | Default | Description |
|---|---|---|---|
| `name` | `string` | — | UAMI name (must match Azure's `^[a-z][a-z0-9-]{2,127}$`). |
| `location` | `string` | — | Azure region. |
| `resource_group_name` | `string` | — | RG that owns the UAMI. |
| `tags` | `map(string)` | `{}` | Resource tags. |
| `federated_credentials` | `map(object)` | `{}` | One entry per ServiceAccount.  Each: `{ name?, issuer, service_account_namespace, service_account_name }`.  Subject is constructed inside the module. |
| `role_assignments` | `map(object)` | `{}` | One entry per Azure role grant.  Each: `{ scope, role_definition_name }`. |
| `allow_subscription_scope` | `bool` | `false` | Refuses bare `/subscriptions/<uuid>` scopes unless `true`. |

## Outputs

`id`, `principal_id`, `client_id`, `tenant_id`, `name`, `federated_credentials`
(map of FIC IDs), `role_assignments` (map of role-assignment IDs).

## Guarantees (also encoded in tests/)

1. The federated-credential subject is always
   `system:serviceaccount:<namespace>:<name>` — call sites cannot drift.
2. The audience is always `["api://AzureADTokenExchange"]`.
3. A subscription-scoped role assignment is rejected at plan time unless
   `allow_subscription_scope = true`.
4. The role-assignment scope is exactly the input scope (no normalisation).
5. UAMI naming matches Azure's `^[a-z][a-z0-9-]{2,127}$`.

## Tests

`terraform test` (TF >= 1.6) runs `tests/*.tftest.hcl`.  CI also runs the
static Python contract in `tools/workload_identity_tests/` against the
module source — this is the gate that runs on the project's pinned
Terraform 1.5.x runtime.
