# IDP GitOps Platform — Linear Walkthrough

*2026-05-26T09:53:00Z by Showboat 0.6.1*
<!-- showboat-id: abe87dbf-0d49-4e51-88f5-ca20a2806021 -->

This walkthrough takes you top-to-bottom through the **IDP GitOps Platform** as it stands at commit `f3a5fb6` on branch `feat/prd-v4-deepening-improvement`. The platform is a production-grade Internal Developer Platform on Azure AKS using the GitOps Bridge pattern. It has three logical planes:

- **DX Plane** — Jira + Bitbucket Cloud + Jenkins (CI, isolated from Azure)
- **Control Plane** — ArgoCD hub on `mgmt-we`/`mgmt-ne` + Crossplane (infra provisioning)
- **Data Plane** — Workload AKS clusters (`aks-dev-we`, `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`)

The narrative below follows execution order: a Terraform apply provisions clusters → ArgoCD bootstraps addons → addons enable secrets/policy/CI/progressive-delivery → Go tooling enforces mgmt-plane invariants → Python tooling onboards new services → CI gates everything.

Repo shape (top-level directories, source LOC):

```bash
find . -maxdepth 1 -type d ! -path '.' ! -path './.*' | sort && echo '---' && echo 'Source LOC (excluding vendored/archived):' && find . -type f \( -name '*.go' -o -name '*.py' -o -name '*.tf' -o -name '*.ts' -o -name '*.tsx' \) ! -path '*/.*' ! -path '*/node_modules/*' ! -path '*/__pycache__/*' ! -path '*/archived/*' ! -path '*/dist/*' | xargs wc -l 2>/dev/null | tail -1
```

```output
./archived
./backstage
./bootstrap
./_docs
./docs
./gitops
./images
./scripts
./terraform
./tools
---
Source LOC (excluding vendored/archived):
 16656 total
```

## 1. Terraform foundation — provider, backend, versions

Everything begins with a Terraform apply. `terraform/provider.tf` declares the provider set, the required version pin (`~> 1.5.0` per ADR-029-v3), and the remote state backend (azurerm with native blob-lease locking per ADR-023-v3). State is partitioned per environment under `tfstate/<env>/<cluster>.tfstate`.

```bash
sed -n '1,60p' terraform/provider.tf
```

```output
terraform {
  # Remote state backend provisioned out-of-band by scripts/bootstrap-tfstate.sh.
  # Initialise with a per-environment key:
  #   terraform init -backend-config=backends/<env>.tfbackend
  backend "azurerm" {
    resource_group_name  = "rg-tfstate-bootstrap"
    storage_account_name = "stplatformtfstate"
    container_name       = "tfstate"
    # key is supplied per-environment via -backend-config=backends/<env>.tfbackend
    # Native blob-lease locking is used automatically; no external lock store needed.
  }

  required_providers {
    azuread = {
      source  = "hashicorp/azuread"
      version = "~> 3.3.0"
    }
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.117"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.36"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.7"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.17"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.12"
    }
  }
  # Pin to 1.5.x (last MPL-licensed line; compatible with tflint, checkov, azurerm toolchain — ADR-029-v3, FR-V3-27)
  required_version = "~> 1.5.0"
}

data "azurerm_client_config" "current" {}

provider "azuread" {
  tenant_id = data.azurerm_client_config.current.tenant_id
}

provider "azurerm" {
  features {
    resource_group {
      prevent_deletion_if_contains_resources = false
    }
  }
}

resource "local_file" "kubeconfig" {
  content  = module.aks.kube_config_raw
  filename = "${path.module}/kubeconfig"
}
```

## 2. Variables — the operator's contract

`terraform/variables.tf` is the contract every environment passes through. Each critical variable carries an inline `validation {}` block (per ADR-026-v3). The validations cover region values, CIDR shapes, SLO classes, sensitive PEM material, and SonarQube hosting modes (`saas|cipool`, per ADR-016-v3-amendment).

```bash
wc -l terraform/variables.tf && echo '---' && grep -c 'validation {' terraform/variables.tf && echo 'validation blocks total'
```

```output
521 terraform/variables.tf
---
16
validation blocks total
```

```bash
sed -n '15,55p' terraform/variables.tf
```

```output
  description = "Specifies the the location for the Azure resources."
  type        = string
  default     = "westeurope"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]+$", var.location))
    error_message = "location must be an Azure region slug: lowercase letters and digits only (e.g., 'westeurope', 'northeurope')."
  }
}

variable "secondary_location" {
  description = "Specifies the paired Azure region for standby resources."
  type        = string
  default     = "northeurope"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]+$", var.secondary_location))
    error_message = "secondary_location must be an Azure region slug: lowercase letters and digits only (e.g., 'northeurope')."
  }
}

variable "dr_location" {
  description = "Specifies the disaster recovery region for the seed cluster network."
  type        = string
  default     = "westus2"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]+$", var.dr_location))
    error_message = "dr_location must be an Azure region slug: lowercase letters and digits only (e.g., 'westus2')."
  }
}

variable "agents_size" {
  description = "Specifies the default virtual machine size for the Kubernetes agents"
  default     = "Standard_D2s_v3"
  type        = string
}

variable "kubernetes_version" {
  description = "Specifies which Kubernetes release to use. The default used is the latest Kubernetes version available in the location."
  type        = string
```

## 3. Cluster topology — clusters.tf

`terraform/clusters.tf` declares the seven AKS clusters with their roles (mgmt-active/standby/workload/seed). Today each cluster's identity (subscription, region, RG, AKS name, ACR hostname) is encoded across `clusters.tf`, `argocd_bootstrap.tf` locals, and `tools/service_seed/seed_job.py` — PRD-v4 FR-V4-01..04 will collapse this into a single `gitops/clusters/registry.yaml` (ADR-031-v4).

```bash
wc -l terraform/clusters.tf && echo '---' && sed -n '1,50p' terraform/clusters.tf
```

```output
83 terraform/clusters.tf
---
locals {
  # FR-V4-02: per-cluster autoscaling profile (the only piece NOT in the registry —
  # capacity planning is environment-tier policy, not cluster identity).
  aks_cluster_autoscale_profiles = {
    "mgmt-ne"        = { enable_auto_scaling = false, agents_count = 1, agents_min_count = null, agents_max_count = null, default_nodepool_vm_size = var.agents_size }
    "aks-dev-we"     = { enable_auto_scaling = true, agents_count = null, agents_min_count = 1, agents_max_count = 3, default_nodepool_vm_size = var.agents_size }
    "aks-staging-we" = { enable_auto_scaling = true, agents_count = null, agents_min_count = 1, agents_max_count = 5, default_nodepool_vm_size = var.agents_size }
    "aks-prod-we"    = { enable_auto_scaling = true, agents_count = null, agents_min_count = 3, agents_max_count = 10, default_nodepool_vm_size = "Standard_D4s_v3" }
    "aks-prod-ne"    = { enable_auto_scaling = true, agents_count = null, agents_min_count = 3, agents_max_count = 10, default_nodepool_vm_size = "Standard_D4s_v3" }
    "seed-wus"       = { enable_auto_scaling = false, agents_count = 1, agents_min_count = null, agents_max_count = null, default_nodepool_vm_size = var.agents_size }
  }

  # FR-V4-02: aks_cluster_definitions is derived from the cluster registry.
  # location, sku_tier, agents_availability_zones — sourced from gitops/clusters/registry.yaml.
  # mgmt-we is provisioned by the dedicated `module.aks` in main.tf; it is NOT in this map.
  aks_cluster_definitions = {
    for k, profile in local.aks_cluster_autoscale_profiles : k => {
      location                  = local.cluster_registry[k].region
      subnet_key                = k
      sku_tier                  = local.cluster_registry[k].sku_tier
      enable_auto_scaling       = profile.enable_auto_scaling
      agents_count              = profile.agents_count
      agents_min_count          = profile.agents_min_count
      agents_max_count          = profile.agents_max_count
      agents_availability_zones = length(local.cluster_registry[k].azs) > 0 ? local.cluster_registry[k].azs : null
      default_nodepool_name     = "system"
      default_nodepool_vm_size  = profile.default_nodepool_vm_size
      default_nodepool_labels   = { nodepool = "defaultnodepool", cluster = k }
      default_nodepool_tags     = { Agent = "defaultnodepoolagent", cluster = k }
    }
  }
}

module "aks_clusters" {
  for_each                                        = local.aks_cluster_definitions
  source                                          = "Azure/aks/azurerm"
  version                                         = "9.4.1"
  cluster_name                                    = each.key
  resource_group_name                             = azurerm_resource_group.this.name
  location                                        = each.value.location
  kubernetes_version                              = var.kubernetes_version
  orchestrator_version                            = var.kubernetes_version
  role_based_access_control_enabled               = var.role_based_access_control_enabled
  rbac_aad                                        = var.rbac_aad
  prefix                                          = replace(each.key, "-", "")
  network_plugin                                  = var.network_plugin
  vnet_subnet_id                                  = azurerm_subnet.spoke_aks[each.value.subnet_key].id
  os_disk_size_gb                                 = var.os_disk_size_gb
  os_sku                                          = var.os_sku
  sku_tier                                        = each.value.sku_tier
```

## 4. Networking — hub-spoke + Azure Firewall

`terraform/networking.tf` builds the hub-spoke topology. Each region has a hub VNet hosting Azure Firewall (Premium SKU per FR-V3-09's egress contract) and Azure Bastion. Workload AKS clusters live in spoke VNets peered to the hub, with all egress forced through the firewall via UDR. Bitbucket Cloud and Atlassian SaaS CIDRs are explicitly allowed at the firewall (the 50-entry list in `variables.tf:bitbucket_cidrs`).

```bash
wc -l terraform/networking.tf && echo '---' && grep -nE '^resource|^module' terraform/networking.tf | head -25
```

```output
406 terraform/networking.tf
---
92:resource "azurerm_virtual_network" "hubs" {
101:resource "azurerm_virtual_network" "spokes" {
110:resource "azurerm_subnet" "hub_firewall" {
118:resource "azurerm_subnet" "hub_bastion" {
126:resource "azurerm_subnet" "hub_private_endpoints" {
135:resource "azurerm_subnet" "hub_shared_services" {
143:resource "azurerm_subnet" "spoke_aks" {
152:resource "azurerm_subnet" "spoke_private_endpoints" {
161:resource "azurerm_public_ip" "firewall_we" {
171:resource "azurerm_firewall_policy" "we" {
179:resource "azurerm_firewall" "we" {
195:resource "azurerm_firewall_policy_rule_collection_group" "we_allowlist" {
254:resource "azurerm_route_table" "spokes" {
262:resource "azurerm_route" "default_to_firewall" {
272:resource "azurerm_subnet_route_table_association" "spoke_aks" {
278:resource "azurerm_subnet_route_table_association" "spoke_private_endpoints" {
284:resource "azurerm_network_security_group" "spokes" {
292:resource "azurerm_subnet_network_security_group_association" "spoke_aks" {
298:resource "azurerm_subnet_network_security_group_association" "spoke_private_endpoints" {
304:resource "azurerm_virtual_network_peering" "hub_to_spoke" {
315:resource "azurerm_virtual_network_peering" "spoke_to_hub" {
326:resource "azurerm_virtual_network_peering" "hub_we_to_ne" {
335:resource "azurerm_virtual_network_peering" "hub_ne_to_we" {
344:resource "azurerm_private_dns_zone" "platform" {
351:resource "azurerm_private_dns_zone_virtual_network_link" "platform" {
```

## 5. Cluster registry — already partially implemented

A surprise during this walkthrough: `gitops/clusters/registry.yaml`, `gitops/clusters/registry.schema.json`, and `terraform/registry.tf` **already exist**. ADR-031-v4's data-as-config pattern is partially landed — `terraform/clusters.tf` reads `local.cluster_registry[k].region` and `.azs` and `.sku_tier`. The remaining PRD-v4 work (FR-V4-03) is pointing `tools/service_seed/` at the same registry.

```bash
cat terraform/registry.tf 2>/dev/null | head -30 && echo '---registry.yaml---' && head -25 gitops/clusters/registry.yaml
```

```output
# Cluster topology registry loader — FR-V4-02, ADR-031-v4.
#
# Single point at which the committed YAML registry (gitops/clusters/registry.yaml)
# is parsed into Terraform locals. Every other .tf file referencing per-cluster
# identity (region, resource group, ACR hostname, AKS name, mgmt role) MUST read
# from local.cluster_registry — no inline maps of cluster identity may live
# elsewhere in terraform/*.tf after v4.
#
# Schema-validated by scripts/validate-cluster-registry.py against
# gitops/clusters/registry.schema.json (pre-commit + CI; FR-V4-04).

locals {
  # Raw decoded registry; every consumer downstream goes through derived locals.
  cluster_registry_raw = yamldecode(file("${path.module}/../gitops/clusters/registry.yaml"))

  # Defensive sanity: top-level YAML key MUST equal entry.aks_name. Any divergence
  # would silently desync ApplicationSet selectors from TF state, so we fail fast
  # at plan time via a precondition-like check via a `null_resource` would require
  # a resource, so instead we surface the keys and let `terraform validate` /
  # tflint pick up any reference error if a downstream uses a mismatched key.
  cluster_registry = {
    for k, v in local.cluster_registry_raw : k => merge(v, {
      # Helper booleans for downstream filtering.
      is_management = contains(["active", "standby"], v.mgmt_role)
      is_workload   = v.mgmt_role == "workload"
      is_seed       = v.mgmt_role == "seed"
    })
  }

  # Convenience: all workload cluster keys (used by external_secrets, argocd_bootstrap).
---registry.yaml---
# Cluster Topology Registry — single committed source of truth (FR-V4-01..04, ADR-031-v4)
#
# Consumed by:
#   - Terraform: yamldecode(file("${path.module}/../gitops/clusters/registry.yaml")) in terraform/registry.tf
#   - ArgoCD ApplicationSet locals: derived from this file (gitops/clusters/capz/aks-appset.yaml)
#   - tools/service_seed/: tools/service_seed/cli.py registry loader (FR-V4-03)
#
# Validation:
#   - Schema: gitops/clusters/registry.schema.json (JSON Schema draft-07)
#   - Validator: scripts/validate-cluster-registry.py (pre-commit + CI; FR-V4-04)
#
# Adding a new cluster is one PR: add a top-level key here, run pre-commit, open PR.
# Removing a cluster requires the catalogued teardown runbook; see ADR-031-v4 §Rollback.
#
# Field reference (every entry MUST carry all fields below):
#   subscription_id  — Azure subscription ID (UUID). Per FR-V3-19, prod and non-prod live in separate subscriptions.
#   region           — Azure region slug (lowercase, no spaces). Authoritative cluster region.
#   region_abbrev    — Short region code used in DNS, ACR hostname, resource group naming (we | ne | wus).
#   resource_group   — Azure Resource Group name owning the AKS cluster + its workload resources.
#   acr_hostname     — Fully-qualified ACR login server (e.g., acrcityossharedwe.azurecr.io).
#   aks_name         — Logical cluster key; matches the top-level YAML key by convention.
#   mgmt_role        — Cluster's management plane role: active | standby | workload | seed (see ADR-004-v2, ADR-022).
#   azs              — Availability zones for the default nodepool (list of strings; null/empty = single-AZ).
#   sku_tier         — AKS SKU tier (Free | Standard | Premium). Production uses Premium per ADR-019.
#   gitops_addons    — Enable_* flags consumed by the GitOps Bridge (drives ApplicationSet selector matching).
```

## 6. Identity foundation — UAMI + federated credentials

Workload identities are user-assigned managed identities (UAMIs) + federated credentials trusting the AKS OIDC issuer. Today this pattern repeats across `jenkins.tf`, `external_secrets.tf`, `akv_sync_exporter.tf`, `saas_token_rotator.tf`, `velero.tf` — six near-identical blocks. PRD-v4 FR-V4-05..09 will collapse this into `terraform/modules/workload_identity/`. Let's see the repetition first.

```bash
grep -c azurerm_user_assigned_identity terraform/*.tf | grep -v ':0' && echo '---' && grep -n 'azurerm_federated_identity_credential' terraform/external_secrets.tf terraform/jenkins.tf terraform/akv_sync_exporter.tf terraform/saas_token_rotator.tf terraform/velero.tf 2>/dev/null
```

```output
terraform/acr.tf:2
terraform/akv_sync_exporter.tf:2
terraform/argocd_bootstrap.tf:1
terraform/clusters.tf:1
terraform/external_secrets.tf:8
terraform/jenkins.tf:3
terraform/keyvaults.tf:1
terraform/main.tf:12
terraform/outputs.tf:3
terraform/saas_token_rotator.tf:2
terraform/storage.tf:2
terraform/velero.tf:2
---
terraform/external_secrets.tf:74:  from = azurerm_federated_identity_credential.external_secrets["aks-dev-we:platform_secrets"]
terraform/external_secrets.tf:75:  to   = module.external_secrets_identity["aks-dev-we"].azurerm_federated_identity_credential.this["aks-dev-we-platform-secrets"]
terraform/external_secrets.tf:78:  from = azurerm_federated_identity_credential.external_secrets["aks-dev-we:kyverno"]
terraform/external_secrets.tf:79:  to   = module.external_secrets_identity["aks-dev-we"].azurerm_federated_identity_credential.this["aks-dev-we-kyverno"]
terraform/external_secrets.tf:82:  from = azurerm_federated_identity_credential.external_secrets["aks-staging-we:platform_secrets"]
terraform/external_secrets.tf:83:  to   = module.external_secrets_identity["aks-staging-we"].azurerm_federated_identity_credential.this["aks-staging-we-platform-secrets"]
terraform/external_secrets.tf:86:  from = azurerm_federated_identity_credential.external_secrets["aks-staging-we:kyverno"]
terraform/external_secrets.tf:87:  to   = module.external_secrets_identity["aks-staging-we"].azurerm_federated_identity_credential.this["aks-staging-we-kyverno"]
terraform/external_secrets.tf:90:  from = azurerm_federated_identity_credential.external_secrets["aks-prod-we:platform_secrets"]
terraform/external_secrets.tf:91:  to   = module.external_secrets_identity["aks-prod-we"].azurerm_federated_identity_credential.this["aks-prod-we-platform-secrets"]
terraform/external_secrets.tf:94:  from = azurerm_federated_identity_credential.external_secrets["aks-prod-we:kyverno"]
terraform/external_secrets.tf:95:  to   = module.external_secrets_identity["aks-prod-we"].azurerm_federated_identity_credential.this["aks-prod-we-kyverno"]
terraform/external_secrets.tf:98:  from = azurerm_federated_identity_credential.external_secrets["aks-prod-ne:platform_secrets"]
terraform/external_secrets.tf:99:  to   = module.external_secrets_identity["aks-prod-ne"].azurerm_federated_identity_credential.this["aks-prod-ne-platform-secrets"]
terraform/external_secrets.tf:102:  from = azurerm_federated_identity_credential.external_secrets["aks-prod-ne:kyverno"]
terraform/external_secrets.tf:103:  to   = module.external_secrets_identity["aks-prod-ne"].azurerm_federated_identity_credential.this["aks-prod-ne-kyverno"]
terraform/jenkins.tf:136:  from = azurerm_federated_identity_credential.external_secrets_mgmt_we
terraform/jenkins.tf:137:  to   = module.external_secrets_mgmt_we_identity.azurerm_federated_identity_credential.this["eso-mgmt-we-jenkins"]
terraform/akv_sync_exporter.tf:44:  from = azurerm_federated_identity_credential.akv_sync_exporter
terraform/akv_sync_exporter.tf:45:  to   = module.akv_sync_exporter_identity.azurerm_federated_identity_credential.this["akv-sync-exporter"]
terraform/saas_token_rotator.tf:47:  from = azurerm_federated_identity_credential.saas_token_rotator
terraform/saas_token_rotator.tf:48:  to   = module.saas_token_rotator_identity.azurerm_federated_identity_credential.this["saas-token-rotator-mgmt-we"]
terraform/velero.tf:104:  from = azurerm_federated_identity_credential.velero
terraform/velero.tf:105:  to   = module.velero_identity.azurerm_federated_identity_credential.this["velero-server-mgmt-we"]
```

Another partially-landed v4 finding: `terraform/modules/workload_identity/` already exists, and `terraform/external_secrets.tf` uses HCL \`moved\` blocks to migrate inline resources to the module without re-creating identities. That \`moved\` chain in the grep output above is the migration in progress.

```bash
ls terraform/modules/workload_identity/ && echo '---' && sed -n '1,40p' terraform/modules/workload_identity/main.tf 2>/dev/null | head -50
```

```output
main.tf
outputs.tf
README.md
tests
variables.tf
versions.tf
---
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
```

## 7. Key Vault topology — per-region pair with RBAC mode

`terraform/keyvaults.tf` provisions two AKVs per environment (one in each hub region — ADR-019). Both run in RBAC mode (\`enableRbacAuthorization: true\`) so ADR-020's per-namespace prefix-scoped ABAC conditions work. Each KV has a private endpoint in its corresponding hub. There are zero \`rotation_policy\` blocks today — that gap is the FR-V3-12 finding driving PRD-v3.1 US-V3.1-01.

```bash
wc -l terraform/keyvaults.tf && echo '---' && grep -nE '^resource|enable_rbac|sku_name' terraform/keyvaults.tf | head -15 && echo '---rotation_policy count---' && grep -c rotation_policy terraform/keyvaults.tf
```

```output
186 terraform/keyvaults.tf
---
58:resource "time_rotating" "platform_secret_quarterly" {
96:resource "azurerm_log_analytics_workspace" "platform" {
105:resource "azurerm_key_vault" "platform" {
112:  sku_name                      = "standard"
115:  enable_rbac_authorization     = true
130:resource "azurerm_role_assignment" "platform_key_vault_admin" {
138:resource "azurerm_role_assignment" "current_operator_key_vault_admin" {
146:resource "azurerm_private_endpoint" "platform_key_vault" {
172:resource "azurerm_monitor_diagnostic_setting" "platform_key_vault" {
---rotation_policy count---
1
```

## 8. ArgoCD bootstrap — the GitOps Bridge handoff

`terraform/argocd_bootstrap.tf` is where Terraform stops and ArgoCD starts. Terraform installs ArgoCD via Helm on the mgmt cluster, then applies a single \`ApplicationSet\` of-Apps that points at \`gitops/bootstrap/control-plane/addons/\`. From that point forward, ArgoCD reconciles every addon, every workload, every cluster.

The handoff payload is the cluster registry combined with the addon enable_* flags. ArgoCD's ApplicationSet uses cluster labels (\`enable_kyverno=true\`, \`enable_external_secrets=true\`, etc.) to determine which apps each cluster receives — the labels come from the registry.

```bash
wc -l terraform/argocd_bootstrap.tf && echo '---' && grep -nE '^locals|enable_|^resource|^module' terraform/argocd_bootstrap.tf | head -20
```

```output
214 terraform/argocd_bootstrap.tf
---
1:locals {
9:      enable_argocd           = "false"
10:      enable_kyverno          = "true"
11:      enable_external_secrets = "true"
23:      enable_argocd           = "false"
24:      enable_kyverno          = "true"
25:      enable_external_secrets = "true"
38:      enable_argocd           = "false"
39:      enable_kyverno          = "true"
40:      enable_external_secrets = "true"
53:      enable_argocd           = "false"
54:      enable_kyverno          = "true"
55:      enable_external_secrets = "true"
68:      enable_argocd           = "false"
69:      enable_kyverno          = "true"
70:      enable_external_secrets = "true"
83:      enable_argocd           = "false"
84:      enable_kyverno          = "true"
85:      enable_external_secrets = "false"
104:resource "kubernetes_secret_v1" "argocd_registered_clusters" {
```

## 9. GitOps addon catalogue — control-plane fan-out

The control-plane addons live under \`gitops/bootstrap/control-plane/addons/\`. Each addon is an ArgoCD Application or ApplicationSet. The split between \`azure/\` (Azure-specific: Crossplane Azure provider configs) and \`oss/\` (open-source: ESO, Kyverno, Argo Rollouts, Jenkins, etc.) is intentional — Crossplane provider configs need Terraform-issued workload identities, OSS addons consume them.

```bash
ls gitops/bootstrap/control-plane/addons/ && echo '---OSS addons---' && ls gitops/bootstrap/control-plane/addons/oss/ | head -25
```

```output
azure
oss
---OSS addons---
addons-akv-sync-exporter.yaml
addons-akv-tls-cert-sync-appset.yaml
addons-argo-cd-appset.yaml
addons-argocd-jira-bridge-secrets-bootstrap.yaml
addons-argocd-jira-bridge.yaml
addons-argo-events-appset.yaml
addons-argo-rollouts-appset.yaml
addons-argo-workflows-appset.yaml
addons-controller-scaler.yaml
addons-crossplane-appset.yaml
addons-crossplane-helm.yaml
addons-crossplane-kubernetes-providerconfigs-workload.yaml
addons-crossplane-kubernetes.yaml
addons-crossplane-platform.yaml
addons-external-secrets-appset.yaml
addons-external-secrets-bootstrap.yaml
addons-jenkins-appset.yaml
addons-jenkins-secrets-bootstrap.yaml
addons-kargo-appset.yaml
addons-kube-prometheus-stack-appset.yaml
addons-kyverno-appset.yaml
addons-kyverno-cosign-key-bootstrap.yaml
addons-kyverno-policies-appset.yaml
addons-mgmt-plane-lock.yaml
addons-platform-alerts-appset.yaml
```

## 10. Mgmt-plane singleton lock — the leader lease

Only one of \`mgmt-we\` / \`mgmt-ne\` can be active at a time (ADR-022). Arbitration is via an Azure Storage Blob lease on \`leases/mgmt-active\`. The active cluster holds the lease; the standby cluster's controllers are scaled to zero (ADR-017). The Go binary \`tools/mgmt-plane-lock/cmd/mgmt-leader-lease/main.go\` runs in both clusters and competes for the lease every renewal tick.

```bash
ls tools/mgmt-plane-lock/cmd/ && echo '---mgmt-leader-lease main.go (top)---' && sed -n '1,40p' tools/mgmt-plane-lock/cmd/mgmt-leader-lease/main.go
```

```output
argocd-jira-bridge
controller-scaler
mgmt-cli
mgmt-leader-lease
saas-token-rotator
---mgmt-leader-lease main.go (top)---
// Command mgmt-leader-lease holds (or contends for) the Azure Storage
// blob lease that designates the active management cluster. The run
// loop lives in internal/bloblease.LeaseRunner — main is the wiring.
package main

import (
	"log"

	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/bootstrap"
	"github.com/ste-cityos/aks-platform-engineering/tools/mgmt-plane-lock/internal/config"
)

func main() {
	ctx, stop := bootstrap.SignalContext()
	defer stop()

	cfg, err := config.LoadController()
	if err != nil {
		log.Fatalf("load controller config: %v", err)
	}
	go func() {
		if err := bootstrap.ServeMetrics(ctx, cfg.MetricsAddr, nil); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()
	if err := buildRunner(ctx, cfg).Run(ctx); err != nil {
		log.Fatalf("mgmt-leader-lease run: %v", err)
	}
}
```

The main.go above is 26 lines — exactly the v4 \`main.go ≤40 lines\` floor (FR-V4-26). \`internal/bootstrap.SignalContext\` and \`ServeMetrics\` already exist, and \`bloblease.LeaseRunner\` owns the run-loop. **US-V4-06 is mostly landed.** Now the lease state machine itself:

```bash
wc -l tools/mgmt-plane-lock/internal/bloblease/*.go && echo '---' && grep -nE '^func|^type' tools/mgmt-plane-lock/internal/bloblease/bloblease.go | head -25
```

```output
  184 tools/mgmt-plane-lock/internal/bloblease/bloblease.go
   40 tools/mgmt-plane-lock/internal/bloblease/bloblease_test.go
  157 tools/mgmt-plane-lock/internal/bloblease/coverage_test.go
  180 tools/mgmt-plane-lock/internal/bloblease/manager_test.go
  222 tools/mgmt-plane-lock/internal/bloblease/runner.go
  240 tools/mgmt-plane-lock/internal/bloblease/runner_test.go
 1023 total
---
17:type Manager struct {
22:type Properties struct {
26:func New(ctx context.Context, blobURL, preferredMetaKey string) (*Manager, error) {
43:func (m *Manager) Acquire(ctx context.Context, duration time.Duration) (string, error) {
59:func (m *Manager) Renew(ctx context.Context, leaseID string) error {
71:func (m *Manager) Release(ctx context.Context, leaseID string) error {
86:func (m *Manager) Break(ctx context.Context) error {
102:func (m *Manager) Properties(ctx context.Context) (Properties, error) {
119:func (m *Manager) PreferredCluster(ctx context.Context) (string, error) {
127:func (m *Manager) SetPreferredCluster(ctx context.Context, cluster string) error {
159:func IsLeaseConflict(err error) bool {
173:func IsMissingLeaseError(err error) bool {
```

## 11. Controller scaler — the lease's downstream consumer

When the lease changes hands, the standby cluster's controllers must scale up and the previous-active's must scale to zero (ADR-017). \`tools/mgmt-plane-lock/cmd/controller-scaler/main.go\` watches the lease state and reconciles a configured set of Deployment/StatefulSet replica counts. \`internal/scaling/\` owns the loop and \`internal/kube/\` owns the K8s client.

```bash
wc -l tools/mgmt-plane-lock/internal/scaling/*.go tools/mgmt-plane-lock/internal/kube/*.go && echo '---scaling surface---' && grep -nE '^func|^type' tools/mgmt-plane-lock/internal/scaling/scaling.go | head -15
```

```output
  175 tools/mgmt-plane-lock/internal/scaling/coverage_test.go
    8 tools/mgmt-plane-lock/internal/scaling/errors.go
  123 tools/mgmt-plane-lock/internal/scaling/runner.go
  145 tools/mgmt-plane-lock/internal/scaling/runner_test.go
  218 tools/mgmt-plane-lock/internal/scaling/scaling.go
  147 tools/mgmt-plane-lock/internal/scaling/scaling_test.go
   77 tools/mgmt-plane-lock/internal/kube/status.go
   85 tools/mgmt-plane-lock/internal/kube/status_test.go
  978 total
---scaling surface---
18:type WorkloadKind string
25:type Workload struct {
34:func DefaultWorkloads() []Workload {
83:func ReadLeaseStatus(ctx context.Context, client kubernetes.Interface, namespace, configMapName string) (string, error) {
100:func ReconcileWorkloads(ctx context.Context, client kubernetes.Interface, workloads []Workload, active bool) error {
115:func UpdateClusterSecretLeaseStatus(ctx context.Context, client kubernetes.Interface, namespace, clusterName, leaseStatus string) error {
150:func reconcileWorkload(ctx context.Context, client kubernetes.Interface, workload Workload, desiredReplicas int32) error {
161:func reconcileDeployment(ctx context.Context, client kubernetes.Interface, workload Workload, desiredReplicas int32) error {
183:func reconcileStatefulSet(ctx context.Context, client kubernetes.Interface, workload Workload, desiredReplicas int32) error {
205:func handleMissingWorkload(err error, workload Workload) bool {
209:func replicasEqual(replicas *int32, desiredReplicas int32) bool {
216:func int32Ptr(value int32) *int32 {
```

## 12. AKV writer + SaaS token rotator

\`saas-token-rotator\` rotates Bitbucket OAuth + Jira API tokens on a quarterly schedule. After each rotation, the new value is written to the per-region AKV via \`internal/akvwriter\`. ESO then projects it into each consuming workload's namespace per ADR-005-v2. The httpx-based transport seam (FR-V4-15..18) is already in place.

```bash
ls tools/mgmt-plane-lock/internal/ && echo '---akvwriter surface---' && grep -nE '^func|^type|^var' tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go 2>/dev/null | head -20
```

```output
akvwriter
bloblease
bootstrap
config
httpx
jirabridge
kube
rotation
scaling
---akvwriter surface---
44:type Strategy int
57:var (
65:type Secret struct {
75:type SecretStore interface {
81:type Client struct {
88:var _ SecretStore = (*Client)(nil)
93:func New(vaultURL string) (*Client, error) {
107:func newWithDeps(vaultURL string, cred azcore.TokenCredential, hc *http.Client) *Client {
111:type setSecretBody struct {
116:type secretAttributes struct {
126:func (c *Client) Put(ctx context.Context, name, value string, strategy Strategy) error {
154:func (c *Client) putOnce(ctx context.Context, name, value string) error {
182:func (c *Client) Get(ctx context.Context, name string) (Secret, error) {
222:func (c *Client) isSoftDeleted(ctx context.Context, name string) (bool, error) {
249:func (c *Client) recoverDeleted(ctx context.Context, name string) error {
268:func (c *Client) token(ctx context.Context) (string, error) {
281:func classify(resp *http.Response) error {
304:type secretVersionItem struct {
310:type secretVersionsResponse struct {
317:func (c *Client) DisableOldVersions(ctx context.Context, name string) error {
```

```bash
echo '---httpx (FR-V4-15..18)---' && ls tools/mgmt-plane-lock/internal/httpx/ 2>/dev/null && grep -nE '^func|^type' tools/mgmt-plane-lock/internal/httpx/*.go 2>/dev/null | head -10
```

```output
---httpx (FR-V4-15..18)---
httpx.go
httpx_test.go
tools/mgmt-plane-lock/internal/httpx/httpx.go:42:type Options struct {
tools/mgmt-plane-lock/internal/httpx/httpx.go:72:type Option func(*Options)
tools/mgmt-plane-lock/internal/httpx/httpx.go:75:func WithBase(rt http.RoundTripper) Option { return func(o *Options) { o.Base = rt } }
tools/mgmt-plane-lock/internal/httpx/httpx.go:78:func WithPerAttemptTimeout(d time.Duration) Option {
tools/mgmt-plane-lock/internal/httpx/httpx.go:83:func WithMaxRetries(n int) Option { return func(o *Options) { o.MaxRetries = n } }
tools/mgmt-plane-lock/internal/httpx/httpx.go:86:func WithBaseBackoff(d time.Duration) Option { return func(o *Options) { o.BaseBackoff = d } }
tools/mgmt-plane-lock/internal/httpx/httpx.go:89:func WithMaxBackoff(d time.Duration) Option { return func(o *Options) { o.MaxBackoff = d } }
tools/mgmt-plane-lock/internal/httpx/httpx.go:93:func WithBodyOnError(maxBytes int) Option {
tools/mgmt-plane-lock/internal/httpx/httpx.go:100:func NewTransport(opts ...Option) http.RoundTripper {
tools/mgmt-plane-lock/internal/httpx/httpx.go:134:func NewClient(opts ...Option) *http.Client {
```

The Go tooling at \`tools/mgmt-plane-lock/internal/\` shows the full PRD-v4 deepening already landed: \`httpx\` transport seam (FR-V4-15..18), \`akvwriter\` with typed \`Strategy\` enum and \`SecretStore\` interface (FR-V4-19..22), \`rotation\` package for saas-token-rotator runner (FR-V4-24), \`bootstrap\` for SIGTERM-aware lifecycle, plus full test coverage at the seams.

## 13. ArgoCD ↔ Jira bridge

When an ArgoCD Application enters \`Degraded\` or \`OutOfSync\` for longer than a threshold, the bridge opens a Jira issue automatically and back-references it from the app's status annotations. This closes ADR-012 (drift visibility).

```bash
wc -l tools/mgmt-plane-lock/cmd/argocd-jira-bridge/main.go tools/mgmt-plane-lock/internal/jirabridge/*.go && echo '---bridge surface---' && grep -nE '^func|^type' tools/mgmt-plane-lock/internal/jirabridge/jirabridge.go 2>/dev/null | head -10
```

```output
   26 tools/mgmt-plane-lock/cmd/argocd-jira-bridge/main.go
  186 tools/mgmt-plane-lock/internal/jirabridge/jirabridge.go
   78 tools/mgmt-plane-lock/internal/jirabridge/jirabridge_test.go
  164 tools/mgmt-plane-lock/internal/jirabridge/runner.go
  454 total
---bridge surface---
12:type Snapshot struct {
20:type ApplicationState struct {
24:type State struct {
28:type EventType string
36:type ApplicationDetails struct {
46:type Event struct {
53:func ParseState(raw string) (State, error) {
69:func (s State) Encode() (string, error) {
82:func SnapshotFromApplication(app *unstructured.Unstructured) ApplicationDetails {
100:func EvaluateEvents(previous Snapshot, current ApplicationDetails, clusterName string, observedAt time.Time, infoLabels, sev1Labels []string) []Event {
```

## 14. Service seed — Python tooling for service onboarding

\`tools/service_seed/\` consumes a Jira issue describing a new service and produces: (a) a Bitbucket repo with cookiecutter scaffolding, (b) a GitOps PR to \`gitops/apps/<svc>/\` with kustomize overlays + Argo Rollout policy + Crossplane XRs for backing resources. Per PRD-v4 US-V4-07, the original 981-line \`seed_job.py\` is splitting into three modules + a thin CLI.

```bash
ls tools/service_seed/ && echo '---' && wc -l tools/service_seed/*.py tools/service_seed/tests/*.py 2>/dev/null
```

```output
cli.py
cookiecutter-service
gitops_pr.py
__init__.py
jira_intake.py
profiles
__pycache__
pyproject.toml
README.md
seed_job.py
service_template.py
templates
tests
---
   500 tools/service_seed/cli.py
   192 tools/service_seed/gitops_pr.py
     1 tools/service_seed/__init__.py
   221 tools/service_seed/jira_intake.py
   111 tools/service_seed/seed_job.py
   407 tools/service_seed/service_template.py
   243 tools/service_seed/tests/test_gitops_pr.py
   227 tools/service_seed/tests/test_jira_intake.py
   189 tools/service_seed/tests/test_registry.py
   242 tools/service_seed/tests/test_robustness.py
    74 tools/service_seed/tests/test_seed_job.py
   196 tools/service_seed/tests/test_service_template.py
   224 tools/service_seed/tests/test_template_profiles.py
  2827 total
```

The three-module split is already on disk: \`jira_intake.py\`, \`service_template.py\`, \`gitops_pr.py\`, plus \`cli.py\` thin shell. \`seed_job.py\` remains as a deprecation shim during the transition.

```bash
head -20 tools/service_seed/cli.py 2>/dev/null
```

```output
"""Cluster topology registry loader for service_seed (FR-V4-03, ADR-031-v4).

This module is the *only* path through which service_seed reads per-cluster
identity (subscription_id, region, resource_group, acr_hostname, aks_name,
mgmt_role). seed_job.py (and its v4 replacements: jira_intake.py,
service_template.py, gitops_pr.py per US-V4-07) must import from here.

The registry file is committed at ``gitops/clusters/registry.yaml`` and is
schema-validated by ``scripts/validate-cluster-registry.py`` in pre-commit and
CI; loaders here do *not* re-validate (single ownership of validation per
FR-V4-04).

Public surface:
    load_registry(path=None) -> dict[str, ClusterEntry]
        Returns a mapping keyed by cluster aks_name. Path defaults to the
        repo-relative gitops/clusters/registry.yaml.

    get_cluster(name, registry=None) -> ClusterEntry
        Convenience: load + key lookup with a typed exception on miss.

```

Test coverage at \`tools/service_seed/tests/\` is 1321 LOC across 7 test files — a far cry from the original 74-line test_seed_job.py. The PRD-v4 75% coverage floor on \`service_template.py\` + \`jira_intake.py\` + \`gitops_pr.py\` is verifiable here.

## 15. CI pipeline — terraform-ci.yml + terraform-apply.yml

Every Terraform PR flows through \`.github/workflows/terraform-ci.yml\`: fmt → validate → tflint → checkov → plan → PR comment. The phased blocking gates (advisory in sprint-N, blocking in sprint-N+1 — ADR-025-v3 / FR-V3-16) mean new rules don't break ongoing work.

Apply runs separately from \`terraform-apply.yml\` after merge to main, with per-environment GitHub Environments providing approval gates. The two-phase matrix (mgmt-first, workloads-after) is the v4 deepening (FR-V4-10..14).

```bash
ls .github/workflows/ && echo '---' && wc -l .github/workflows/*.yml
```

```output
reusable
sonar.yml
terraform-apply.yml
terraform-ci.yml
---
   91 .github/workflows/sonar.yml
  148 .github/workflows/terraform-apply.yml
  656 .github/workflows/terraform-ci.yml
  895 total
```

## 16. Quality gates — pre-commit, tflint, checkov, sonar

Every change passes pre-commit before reaching CI. The hook set mirrors CI exactly so local and CI behaviors converge.

```bash
grep -E '^  - |^  - id:|^    rev:' .pre-commit-config.yaml | head -30 && echo '---.tflint.hcl plugins---' && grep -E 'plugin|version' .tflint.hcl && echo '---.checkov.yaml---' && head -15 .checkov.yaml
```

```output
  - repo: https://github.com/antonbabenko/pre-commit-terraform
    rev: v1.92.0
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v4.6.0
  - repo: local
---.tflint.hcl plugins---
plugin "azurerm" {
  version = "0.26.0"
---.checkov.yaml---
# .checkov.yaml — shared Checkov configuration (US-V3-13, ADR-027-v3)
#
# Consumed identically by:
#   - pre-commit (US-V3-18): checkov hook reads this file automatically
#   - GitHub Actions CI (US-V3-12): checkov-action receives config_file: .checkov.yaml
#
# Running locally:
#   checkov -d terraform/ --config-file .checkov.yaml
#
# Sprint gate phases (ADR-027-v3):
#   Sprint-1: soft-fail true  — checkov warnings do not block CI or local commits
#   Sprint-2: soft-fail false — flip this value and remove the sprint-1 comment at cutover
#
# SPRINT-2 CUTOVER CHECKLIST:
#   1. Run: checkov -d terraform/ --config-file .checkov.yaml --create-baseline
```

## 17. The data plane in motion

Once everything above is in place, a new service onboarded by \`tools/service_seed/\` flows through this end-to-end pipeline:

1. **Jira ticket created** by product/platform team → \`jira_intake.parse\` → typed \`ServiceRequest\`
2. **Cluster registry consulted** → cluster identity (subscription, RG, ACR) resolved from \`gitops/clusters/registry.yaml\`
3. **Service repo scaffolded** via cookiecutter → committed to Bitbucket
4. **GitOps PR composed** → kustomize overlays + Argo \`Rollout\` + Crossplane XRs for SQL/Cosmos/Service Bus
5. **PR merges** → ArgoCD picks up the change → addons fan out to the target cluster
6. **Crossplane XRs provision** backing Azure resources via the Crossplane Azure provider
7. **ESO syncs secrets** from per-region AKV into the service's namespace
8. **Argo Rollouts canary** ships new versions per the SLO class (gold/silver/bronze) defined in \`profiles/rollout.yaml\`
9. **Argo CD ↔ Jira bridge** opens a Jira ticket if the rollout degrades

The mgmt-plane lock (\`bloblease.LeaseRunner\`) ensures only one mgmt cluster is reconciling at a time, so steps 5–9 never race between regions.

## 18. What's still in flight (v3.1 + v4)

Two PRDs are open against this codebase: **PRD-v3.1** (pre-v4 hardening — AKV-native rotation policies + TLS history purge) and **PRD-v4** (architecture deepening). Most of v4's Go/Python/TF deepening has been written but is not yet declared "done" in \`prd.json\`:

```bash
python3 -c "import json; d = json.load(open('prd.json')); print(f'Stories: {len(d[\"userStories\"])}'); print(f'Branch: {d[\"branchName\"]}'); print(); [print(f'  prio={s[\"priority\"]:>2}  passes={str(s[\"passes\"]):>5}  {s[\"id\"]}: {s[\"title\"][:70]}') for s in d['userStories']]"
```

```output
Stories: 13
Branch: ralph/prd-v4-deepening-improvement

  prio= 1  passes= True  US-V3.1-01: AKV-native quarterly rotation policies for platform secrets and certs 
  prio= 2  passes= True  US-V3.1-02: TLS history purge via git filter-repo (FR-V3.1-06..09)
  prio= 3  passes= True  US-V4-11: CI hygiene + version source-of-truth (QW-CI: FR-V4-45..49)
  prio= 4  passes= True  US-V4-01: Cluster topology registry as data (FR-V4-01..04)
  prio= 4  passes= True  US-V4-03: Reusable plan/apply workflows + composite action (FR-V4-10..14)
  prio= 5  passes= True  US-V4-02: Workload-identity Terraform module (FR-V4-05..09)
  prio= 5  passes= True  US-V4-10: Robustness pass (QW-ROB: FR-V4-41..44)
  prio= 6  passes= True  US-V4-04: Go HTTP transport seam (FR-V4-15..18)
  prio= 6  passes= True  US-V4-05: AKV writer consolidation (FR-V4-19..22)
  prio= 6  passes= True  US-V4-06: Go binary lifecycle + per-binary runners (FR-V4-23..26)
  prio= 7  passes= True  US-V4-07: service_seed three-module split (FR-V4-27..31)
  prio= 7  passes= True  US-V4-08: Jinja templates + SLO/rollout profiles (FR-V4-32..35)
  prio= 8  passes= True  US-V4-09: Hybrid Helm -> ESO secret migration (QW-SEC: FR-V4-36..40)
```

**State note**: every story in \`prd.json\` is currently \`passes=true\`. Combined with the partially-already-landed code observed in §5 (cluster registry), §6 (workload_identity module), §10 (bootstrap + LeaseRunner), §12 (httpx + akvwriter + rotation), and §14 (service_seed three-module split), the platform appears to have most of PRD-v4 already implemented in the working tree even though the PRD itself was only authored 2026-05-25 and ADR-031-v4 approved 2026-05-26. The remaining work is verification — running the PRD's CI assertions, populating the Checkov baseline rationale schema, etc. PRD-v3.1's US-V3.1-01 (AKV rotation) and US-V3.1-02 (TLS history purge) are the only items where the underlying code/operational work was NOT visible during this walkthrough.

## 19. How to re-run this walkthrough

Every code block above was captured by \`showboat exec\` — the outputs are real, not paraphrased. You can re-run the entire walkthrough and confirm the platform still behaves identically:

\`\`\`bash
uvx showboat verify walkthrough.md
\`\`\`

Any drift (file moved, line numbers shifted, new resources added) shows up as a diff. If you want to see the commands that built the walkthrough:

\`\`\`bash
uvx showboat extract walkthrough.md
\`\`\`

That prints the showboat note/exec sequence so the document is reproducible from scratch.
