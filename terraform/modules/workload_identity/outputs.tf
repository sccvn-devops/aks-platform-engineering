output "id" {
  description = "Resource ID of the user-assigned managed identity."
  value       = azurerm_user_assigned_identity.this.id
}

output "principal_id" {
  description = "Service principal (object) ID — use for role assignments outside the module."
  value       = azurerm_user_assigned_identity.this.principal_id
}

output "client_id" {
  description = "Client ID — emitted to ArgoCD / Helm consumers via cluster metadata."
  value       = azurerm_user_assigned_identity.this.client_id
}

output "tenant_id" {
  description = "Tenant ID of the UAMI."
  value       = azurerm_user_assigned_identity.this.tenant_id
}

output "name" {
  description = "Name of the UAMI (echoed for convenience)."
  value       = azurerm_user_assigned_identity.this.name
}

output "federated_credentials" {
  description = "Map of federated-credential resource IDs keyed by input key."
  value = {
    for k, fic in azurerm_federated_identity_credential.this :
    k => fic.id
  }
}

output "role_assignments" {
  description = "Map of role-assignment IDs keyed by input key."
  value = {
    for k, ra in azurerm_role_assignment.this :
    k => ra.id
  }
}
