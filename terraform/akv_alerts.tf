# US-V3.1-01 (FR-V3.1-04): AKV near-expiry alerts.
#
# AKV emits Microsoft.KeyVault.SecretNearExpiry and
# Microsoft.KeyVault.CertificateNearExpiry Event Grid events ahead of expiry
# (default 30 days; AKV evaluates the events 24h after a secret expires too).
# The PRD AC names azurerm_monitor_metric_alert, but AKV does not expose
# secret-expiry as a metric — Event Grid is the only Terraform-native primitive
# that fires per-secret-near-expiry. The end-to-end behaviour the AC requires
# (alert sent to platform on-call channel with secret name, vault name, expiry
# date, runbook link) is preserved.
#
# Topology:
#   - One azurerm_eventgrid_system_topic per Key Vault (mgmt-we + 4 workload vaults).
#   - One azurerm_eventgrid_event_subscription per topic, filtered to
#     SecretNearExpiry + CertificateNearExpiry event types, delivering to the
#     platform on-call webhook URL with the runbook link embedded as a static
#     "additionalProperties" payload header.

locals {
  akv_alert_topics = merge(
    {
      "mgmt-we" = {
        vault_id = azurerm_key_vault.management_ci.id
        location = var.location
        cluster  = "mgmt-we"
        hub      = "we"
      }
    },
    {
      for key, vault in local.workload_key_vaults : key => {
        vault_id = azurerm_key_vault.platform[key].id
        location = vault.location
        cluster  = vault.cluster
        hub      = vault.hub
      }
    },
  )
}

resource "azurerm_eventgrid_system_topic" "platform_key_vault" {
  for_each = local.akv_alert_topics

  name                   = "egst-${each.value.cluster}-akv"
  resource_group_name    = azurerm_resource_group.this.name
  location               = each.value.location
  source_arm_resource_id = each.value.vault_id
  topic_type             = "Microsoft.KeyVault.vaults"

  tags = merge(var.tags, {
    service = "akv-near-expiry-alerts"
    cluster = each.value.cluster
    region  = each.value.hub
  })
}

resource "azurerm_eventgrid_event_subscription" "platform_key_vault_near_expiry" {
  for_each = local.akv_alert_topics

  name  = "akv-near-expiry-${each.value.cluster}"
  scope = each.value.vault_id

  included_event_types = [
    "Microsoft.KeyVault.SecretNearExpiry",
    "Microsoft.KeyVault.CertificateNearExpiry",
    "Microsoft.KeyVault.KeyNearExpiry",
  ]

  webhook_endpoint {
    url                               = var.platform_oncall_webhook_url
    max_events_per_batch              = 1
    preferred_batch_size_in_kilobytes = 64
  }

  retry_policy {
    max_delivery_attempts = 24
    event_time_to_live    = 1440 # minutes (24h)
  }

  delivery_property {
    header_name = "X-Platform-Runbook"
    type        = "Static"
    value       = var.platform_oncall_runbook_url
  }

  delivery_property {
    header_name = "X-Platform-Cluster"
    type        = "Static"
    value       = each.value.cluster
  }

  depends_on = [azurerm_eventgrid_system_topic.platform_key_vault]
}
