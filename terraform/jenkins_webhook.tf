locals {
  jenkins_frontdoor_endpoint_name = "afd-jenkins-webhook"
}

resource "azurerm_firewall_policy_rule_collection_group" "we_jenkins_webhook_ingress" {
  name               = "rcg-jenkins-webhook-ingress"
  firewall_policy_id = azurerm_firewall_policy.we.id
  priority           = 200

  nat_rule_collection {
    name     = "nrc-bitbucket-jenkins-webhook"
    priority = 100
    action   = "Dnat"

    rule {
      name                = "bitbucket-webhooks-to-jenkins"
      protocols           = ["TCP"]
      source_addresses    = var.jenkins_webhook_allowed_ipv4_cidrs
      destination_address = azurerm_public_ip.firewall_we.ip_address
      destination_ports   = ["443"]
      translated_address  = var.jenkins_webhook_internal_load_balancer_ip
      translated_port     = "443"
    }
  }
}

resource "azurerm_cdn_frontdoor_profile" "jenkins_webhook" {
  name                = "afd-profile-jenkins-webhook"
  resource_group_name = azurerm_resource_group.this.name
  sku_name            = "Standard_AzureFrontDoor"
  tags                = merge(var.tags, { service = "jenkins-webhook-frontdoor" })
}

resource "azurerm_cdn_frontdoor_endpoint" "jenkins_webhook" {
  name                     = local.jenkins_frontdoor_endpoint_name
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.jenkins_webhook.id
  enabled                  = true
  tags                     = merge(var.tags, { service = "jenkins-webhook-endpoint" })
}

resource "azurerm_cdn_frontdoor_origin_group" "jenkins_webhook" {
  name                     = "og-jenkins-webhook"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.jenkins_webhook.id

  load_balancing {
    additional_latency_in_milliseconds = 0
    sample_size                        = 4
    successful_samples_required        = 2
  }

  health_probe {
    interval_in_seconds = 120
    path                = "/login"
    protocol            = "Https"
    request_type        = "GET"
  }
}

resource "azurerm_cdn_frontdoor_origin" "jenkins_webhook" {
  name                           = "origin-jenkins-webhook"
  cdn_frontdoor_origin_group_id  = azurerm_cdn_frontdoor_origin_group.jenkins_webhook.id
  enabled                        = true
  certificate_name_check_enabled = false
  host_name                      = azurerm_public_ip.firewall_we.ip_address
  http_port                      = 80
  https_port                     = 443
  origin_host_header             = azurerm_public_ip.firewall_we.ip_address
  priority                       = 1
  weight                         = 1000
}

resource "azurerm_cdn_frontdoor_route" "jenkins_webhook" {
  name                          = "route-jenkins-webhook"
  cdn_frontdoor_endpoint_id     = azurerm_cdn_frontdoor_endpoint.jenkins_webhook.id
  cdn_frontdoor_origin_group_id = azurerm_cdn_frontdoor_origin_group.jenkins_webhook.id
  cdn_frontdoor_origin_ids      = [azurerm_cdn_frontdoor_origin.jenkins_webhook.id]
  enabled                       = true
  forwarding_protocol           = "HttpsOnly"
  https_redirect_enabled        = true
  patterns_to_match             = ["/*"]
  supported_protocols           = ["Http", "Https"]
  link_to_default_domain        = true
}

resource "azurerm_cdn_frontdoor_firewall_policy" "jenkins_webhook" {
  name                              = "wafJenkinsWebhook"
  resource_group_name               = azurerm_resource_group.this.name
  sku_name                          = azurerm_cdn_frontdoor_profile.jenkins_webhook.sku_name
  enabled                           = true
  mode                              = "Prevention"
  custom_block_response_status_code = 403
  tags                              = merge(var.tags, { service = "jenkins-webhook-waf" })

  custom_rule {
    name     = "AllowAtlassianBitbucketWebhookCIDRs"
    enabled  = true
    priority = 1
    type     = "MatchRule"
    action   = "Allow"

    match_condition {
      match_variable     = "RemoteAddr"
      operator           = "IPMatch"
      negation_condition = false
      match_values       = var.jenkins_webhook_allowed_ipv4_cidrs
    }
  }

  custom_rule {
    name     = "BlockNonAtlassianWebhookSources"
    enabled  = true
    priority = 100
    type     = "MatchRule"
    action   = "Block"

    match_condition {
      match_variable     = "RemoteAddr"
      operator           = "IPMatch"
      negation_condition = false
      match_values       = ["0.0.0.0/0"]
    }
  }
}

resource "azurerm_cdn_frontdoor_security_policy" "jenkins_webhook" {
  name                     = "spJenkinsWebhook"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.jenkins_webhook.id

  security_policies {
    firewall {
      cdn_frontdoor_firewall_policy_id = azurerm_cdn_frontdoor_firewall_policy.jenkins_webhook.id

      association {
        domain {
          cdn_frontdoor_domain_id = azurerm_cdn_frontdoor_endpoint.jenkins_webhook.id
        }

        patterns_to_match = ["/*"]
      }
    }
  }
}
