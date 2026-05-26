resource "azurerm_key_vault" "management_ci" {
  name                          = "kv-platform-mgmt-we"
  location                      = var.location
  resource_group_name           = azurerm_resource_group.this.name
  tenant_id                     = data.azurerm_client_config.current.tenant_id
  sku_name                      = "standard"
  soft_delete_retention_days    = 90
  purge_protection_enabled      = true
  enable_rbac_authorization     = true
  public_network_access_enabled = false
  tags = merge(var.tags, {
    service = "key-vault"
    cluster = "mgmt-we"
    region  = "we"
  })

  network_acls {
    default_action             = "Deny"
    bypass                     = "None"
    virtual_network_subnet_ids = [azurerm_subnet.spoke_private_endpoints["mgmt-we"].id]
  }
}

resource "azurerm_role_assignment" "management_ci_key_vault_admin" {
  scope                = azurerm_key_vault.management_ci.id
  role_definition_name = "Key Vault Administrator"
  principal_id         = azurerm_user_assigned_identity.akspe.principal_id
}

resource "azurerm_role_assignment" "management_ci_current_operator_admin" {
  scope                = azurerm_key_vault.management_ci.id
  role_definition_name = "Key Vault Administrator"
  principal_id         = data.azurerm_client_config.current.object_id
}

resource "azurerm_key_vault_key" "management_ci_cosign" {
  name         = "cosign-signing-key"
  key_vault_id = azurerm_key_vault.management_ci.id
  key_type     = "RSA"
  key_size     = 4096
  key_opts = [
    "sign",
    "verify",
  ]

  # AKV-native quarterly rotation: new key version auto-created every 90 days.
  # expire_after=P90D pins the key lifetime; automatic rotation triggers at P60D
  # (30 days before expiry) so there is always an overlap window where both the
  # old and new versions are valid for signature verification.
  rotation_policy {
    automatic {
      time_before_expiry = "P30D"
    }
    expire_after         = "P90D"
    notify_before_expiry = "P29D"
  }

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_role_assignment" "jenkins_management_ci_crypto_user" {
  scope                = azurerm_key_vault.management_ci.id
  role_definition_name = "Key Vault Crypto User"
  principal_id         = azurerm_user_assigned_identity.jenkins.principal_id
}

resource "azurerm_private_endpoint" "management_ci_key_vault" {
  name                = "pe-kv-platform-mgmt-we"
  location            = var.location
  resource_group_name = azurerm_resource_group.this.name
  subnet_id           = azurerm_subnet.hub_private_endpoints["we"].id
  tags = merge(var.tags, {
    service = "key-vault-private-endpoint"
    cluster = "mgmt-we"
    region  = "we"
  })

  private_service_connection {
    name                           = "psc-kv-platform-mgmt-we"
    private_connection_resource_id = azurerm_key_vault.management_ci.id
    is_manual_connection           = false
    subresource_names              = ["vault"]
  }

  private_dns_zone_group {
    name                 = "akv-dns"
    private_dns_zone_ids = [azurerm_private_dns_zone.platform["akv"].id]
  }
}

resource "azurerm_monitor_diagnostic_setting" "management_ci_key_vault" {
  name                       = "diag-mgmt-we-ci"
  target_resource_id         = azurerm_key_vault.management_ci.id
  log_analytics_workspace_id = azurerm_log_analytics_workspace.platform.id

  enabled_log {
    category = "AuditEvent"
  }

  metric {
    category = "AllMetrics"
  }
}

resource "azurerm_user_assigned_identity" "external_secrets_mgmt_we" {
  name                = "uami-eso-mgmt-we"
  resource_group_name = azurerm_resource_group.this.name
  location            = var.location
  tags                = merge(var.tags, { cluster = "mgmt-we", purpose = "external-secrets" })
}

resource "azurerm_federated_identity_credential" "external_secrets_mgmt_we" {
  name                = "eso-mgmt-we-jenkins"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks.oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.external_secrets_mgmt_we.id
  subject             = "system:serviceaccount:jenkins:azure-keyvault-reader"

  depends_on = [module.aks]
}

resource "azurerm_role_assignment" "external_secrets_mgmt_we_key_vault_reader" {
  scope                = azurerm_key_vault.management_ci.id
  role_definition_name = "Key Vault Secrets User"
  principal_id         = azurerm_user_assigned_identity.external_secrets_mgmt_we.principal_id
}

resource "azurerm_federated_identity_credential" "jenkins_controller" {
  name                = "jenkins-controller-mgmt-we"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks.oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.jenkins.id
  subject             = "system:serviceaccount:jenkins:jenkins-controller"

  depends_on = [module.aks]
}

resource "azurerm_federated_identity_credential" "jenkins_agent" {
  name                = "jenkins-agent-mgmt-we"
  resource_group_name = azurerm_resource_group.this.name
  audience            = ["api://AzureADTokenExchange"]
  issuer              = module.aks.oidc_issuer_url
  parent_id           = azurerm_user_assigned_identity.jenkins.id
  subject             = "system:serviceaccount:jenkins:jenkins-agent"

  depends_on = [module.aks]
}

resource "azurerm_key_vault_secret" "jenkins_admin_username" {
  name         = "jenkins-admin-username"
  value        = var.jenkins_admin_username
  key_vault_id = azurerm_key_vault.management_ci.id

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "jenkins_admin_password" {
  name            = "jenkins-admin-password"
  value           = var.jenkins_admin_password
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "jenkins_bitbucket_workspace_token" {
  name            = "bitbucket-workspace-token"
  value           = var.jenkins_bitbucket_workspace_token
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "jenkins_jira_service_account_token" {
  name            = "jira-service-account-token"
  value           = var.jenkins_jira_service_account_token
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "jenkins_webhook_https_keystore" {
  name            = "jenkins-webhook-https-keystore"
  value           = var.jenkins_webhook_https_keystore_base64
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "jenkins_webhook_https_keystore_password" {
  name            = "jenkins-webhook-https-keystore-password"
  value           = var.jenkins_webhook_https_keystore_password
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "backstage_postgres_password" {
  name            = "backstage-postgres-password"
  value           = var.postgres_password
  key_vault_id    = azurerm_key_vault.management_ci.id
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "backstage_tls_crt" {
  name            = "backstage-tls-crt"
  value           = var.backstage_tls_crt
  key_vault_id    = azurerm_key_vault.management_ci.id
  content_type    = "application/x-pem-file"
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

resource "azurerm_key_vault_secret" "backstage_tls_key" {
  name            = "backstage-tls-key"
  value           = var.backstage_tls_key
  key_vault_id    = azurerm_key_vault.management_ci.id
  content_type    = "application/x-pem-file"
  expiration_date = local.platform_secret_expiry_rfc3339

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

# US-V3.1-01 (FR-V3.1-01): AKV-issued Backstage TLS certificate with quarterly
# AutoRenew at lifetime_percentage = 75. Coexists with the operator-supplied
# backstage-tls-crt / backstage-tls-key secrets above; consumers should migrate
# to read backstage-internal-tls (PEM bundle) once the cutover ADR lands.
resource "azurerm_key_vault_certificate" "backstage_internal_tls" {
  name         = "backstage-internal-tls"
  key_vault_id = azurerm_key_vault.management_ci.id

  certificate_policy {
    issuer_parameters {
      name = "Self"
    }
    key_properties {
      exportable = true
      key_size   = 2048
      key_type   = "RSA"
      reuse_key  = false
    }
    lifetime_action {
      action {
        action_type = "AutoRenew"
      }
      trigger {
        lifetime_percentage = 75
      }
    }
    secret_properties {
      content_type = "application/x-pem-file"
    }
    x509_certificate_properties {
      extended_key_usage = ["1.3.6.1.5.5.7.3.1"]
      key_usage          = ["digitalSignature", "keyEncipherment"]
      subject            = "CN=backstage.internal"
      validity_in_months = 3
      subject_alternative_names {
        dns_names = ["backstage.internal", "*.backstage.internal"]
      }
    }
  }

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}

# US-V3.1-01 (FR-V3.1-01): AKV-issued Jenkins webhook TLS certificate with
# quarterly AutoRenew at lifetime_percentage = 75. Jenkins reads the PEM bundle
# (or PKCS12 derivative) from this cert via the operator-supplied
# jenkins-webhook-https-keystore* secrets once those are deprecated.
resource "azurerm_key_vault_certificate" "jenkins_webhook_tls" {
  name         = "jenkins-webhook-tls"
  key_vault_id = azurerm_key_vault.management_ci.id

  certificate_policy {
    issuer_parameters {
      name = "Self"
    }
    key_properties {
      exportable = true
      key_size   = 2048
      key_type   = "RSA"
      reuse_key  = false
    }
    lifetime_action {
      action {
        action_type = "AutoRenew"
      }
      trigger {
        lifetime_percentage = 75
      }
    }
    secret_properties {
      content_type = "application/x-pem-file"
    }
    x509_certificate_properties {
      extended_key_usage = ["1.3.6.1.5.5.7.3.1"]
      key_usage          = ["digitalSignature", "keyEncipherment"]
      subject            = "CN=jenkins-webhook.internal"
      validity_in_months = 3
      subject_alternative_names {
        dns_names = ["jenkins-webhook.internal"]
      }
    }
  }

  depends_on = [
    azurerm_role_assignment.management_ci_key_vault_admin,
    azurerm_role_assignment.management_ci_current_operator_admin,
  ]
}
