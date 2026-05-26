variable "name" {
  description = "Name of the user-assigned managed identity (e.g. uami-velero)."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,127}$", var.name))
    error_message = "name must match ^[a-z][a-z0-9-]{2,127}$ (Azure UAMI naming rules)."
  }
}

variable "location" {
  description = "Azure region of the UAMI (typically the same region as the consuming AKS cluster)."
  type        = string
}

variable "resource_group_name" {
  description = "Resource group that owns the UAMI + federated credentials."
  type        = string
}

variable "tags" {
  description = "Resource tags applied to the UAMI."
  type        = map(string)
  default     = {}
}

variable "federated_credentials" {
  description = <<-EOT
    Map of federated identity credentials (typically one per ServiceAccount).
    Key is a stable name (used as the AzureAD federated credential resource name
    unless `name` is overridden).  Subject is constructed inside the module as
    `system:serviceaccount:<namespace>:<name>` to prevent drift between call
    sites.
  EOT

  type = map(object({
    name                      = optional(string)
    issuer                    = string
    service_account_namespace = string
    service_account_name      = string
  }))
  default = {}

  validation {
    condition = alltrue([
      for k, v in var.federated_credentials :
      can(regex("^https://", v.issuer))
    ])
    error_message = "federated_credentials[*].issuer must be a fully-qualified https:// URL (the AKS oidc_issuer_url output)."
  }

  validation {
    condition = alltrue([
      for k, v in var.federated_credentials :
      can(regex("^[a-z0-9][a-z0-9-]{0,62}$", v.service_account_namespace)) &&
      can(regex("^[a-z0-9][a-z0-9-]{0,62}$", v.service_account_name))
    ])
    error_message = "federated_credentials[*].service_account_namespace and service_account_name must match Kubernetes RFC1123 name rules."
  }
}

variable "role_assignments" {
  description = <<-EOT
    Map of role assignments granted to the UAMI.  Key is a stable name used
    only in the Terraform address (not the Azure side).  Subscription-scoped
    scopes require `allow_subscription_scope = true` so that broad grants are
    visible at code-review time (FR-V4-08).
  EOT

  type = map(object({
    scope                = string
    role_definition_name = string
  }))
  default = {}
}

variable "allow_subscription_scope" {
  description = <<-EOT
    When false (default), the module refuses role_assignments whose scope is a
    bare /subscriptions/<uuid>.  Set to true at the call site only when a
    subscription-scoped grant is intentional (e.g. CAPZ / Crossplane provider
    identities).
  EOT
  type        = bool
  default     = false
}
