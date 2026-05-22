variable "resource_group_name" {
  description = "Specifies the name of the resource group."
  default     = "aks-gitops"
  type        = string
}

variable "github_token" {
  description = "Specifies the GitHub token for the GitHub repository."
  type        = string
  default     = ""

}

variable "location" {
  description = "Specifies the the location for the Azure resources."
  type        = string
  default     = "westeurope"
}

variable "secondary_location" {
  description = "Specifies the paired Azure region for standby resources."
  type        = string
  default     = "northeurope"
}

variable "dr_location" {
  description = "Specifies the disaster recovery region for the seed cluster network."
  type        = string
  default     = "westus2"
}

variable "agents_size" {
  description = "Specifies the default virtual machine size for the Kubernetes agents"
  default     = "Standard_D2s_v3"
  type        = string
}

variable "kubernetes_version" {
  description = "Specifies which Kubernetes release to use. The default used is the latest Kubernetes version available in the location."
  type        = string
  default     = null
}

variable "green_field_application_gateway_for_ingress" {
  description = "Specifies the Application Gateway for Ingress Controller"
  type        = any
  default     = null
}

variable "create_role_assignments_for_application_gateway" {
  description = "Specifies whether to create role assignments for Application Gateway"
  type        = bool
  default     = true
}

variable "infrastructure_provider" {
  description = "Specific the choice of infrastructure provider. crossplane or capz"
  type        = string
  default     = "capz"
}

variable "addons" {
  description = "Specifies the Kubernetes addons to install on the hub cluster."
  type        = any
  default = {
    enable_argocd = true # installs argocd
  }
}

variable "addons_versions" {
  description = "Specifies the Kubernetes addons to install on the hub cluster."
  type = list(object({
    argocd_chart_version        = string
    argo_rollouts_chart_version = string
    kargo_chart_version         = string
  }))
  default = [{
    argocd_chart_version        = "7.8.25" # https://github.com/argoproj/argo-helm/blob/main/charts/argo-cd/Chart.yaml
    argo_rollouts_chart_version = "2.39.5" # https://github.com/argoproj/argo-helm/blob/main/charts/argo-rollouts/Chart.yaml
    kargo_chart_version         = "1.4.1"  # https://github.com/akuity/kargo/releases
  }]
}

variable "git_private_ssh_key" {
  description = "Filepath to the private SSH key for git access"
  type        = string
  default     = "./private_ssh_deploy_key"
}

variable "git_public_ssh_key" {
  description = "A custom ssh key to control access to the AKS workload cluster(s). This should a string containing the key and not a filepath to the key."
  type        = string
  default     = ""
}

# Addons Git
variable "gitops_addons_org" {
  description = "Specifies the Git repository org/user contains for addons."
  type        = string
  default     = "https://github.com/azure-samples"
}
variable "gitops_addons_repo" {
  description = "Specifies the Git repository contains for addons."
  type        = string
  default     = "aks-platform-engineering"
}
variable "gitops_addons_revision" {
  description = "Specifies the Git repository revision/branch/ref for addons."
  type        = string
  default     = "main"
}
variable "gitops_addons_repo_url" {
  description = "Optional full Git repository URL for Argo CD source and repo credentials. Set this to an SSH URL when using a deploy key."
  type        = string
  default     = ""
}
variable "gitops_addons_basepath" {
  description = "Specifies the Git repository base path for addons."
  type        = string
  default     = "gitops/" # ending slash is important!
}
variable "gitops_addons_path" {
  description = "Specifies the Git repository path for addons."
  type        = string
  default     = "bootstrap/control-plane/addons"
}
variable "tags" {
  description = "Specifies tags for all the resources."
  default = {
    createdWith = "Terraform"
    pattern     = "GitOpsBridge"
  }
}

variable "role_based_access_control_enabled" {
  description = "Is Role Based Access Control Enabled? Changing this forces a new resource to be created."
  type        = bool
  default     = true
}

variable "rbac_aad" {
  description = "Is Role Based Access Control based on Azure AD enabled?"
  type        = bool
  default     = false
}

variable "prefix" {
  description = "Specifies the prefix for the AKS cluster"
  type        = string
  default     = "gitops"
}

variable "network_plugin" {
  description = "Specifies the network plugin of the AKS cluster"
  default     = "azure"
  type        = string
}

variable "os_disk_size_gb" {
  description = "Specifies the OS disk size"
  type        = number
  default     = 50
}

variable "os_sku" {
  type        = string
  default     = "AzureLinux"
  description = "(Optional) Specifies the OS SKU used by the agent pool. Possible values are AzureLinux, Ubuntu, Windows2019 and Windows2022. If not specified, the default is Ubuntu if OSType=Linux or Windows2019 if OSType=Windows. And the default Windows OSSKU will be changed to Windows2022 after Windows2019 is deprecated. Changing this from AzureLinux or Ubuntu to AzureLinux or Ubuntu will not replace the resource, otherwise temporary_name_for_rotation must be specified when attempting a change."
}

variable "sku_tier" {
  description = "Specifies the SKU Tier that should be used for this AKS Cluster."
  type        = string
  default     = "Standard"
}

variable "private_cluster_enabled" {
  description = "Specifies wether the AKS cluster be private or not."
  default     = true
  type        = bool
}

variable "enable_auto_scaling" {
  description = "Specifies whether to enable auto-scaler. Defaults to false."
  type        = bool
  default     = true
}

variable "enable_host_encryption" {
  description = "Specifies whether the nodes in this Node Pool have host encryption enabled. Defaults to false."
  type        = bool
  default     = false
}

variable "log_analytics_workspace_enabled" {
  description = "Specifies whether Log Analytics is enabled"
  type        = bool
  default     = false
}

variable "agents_min_count" {
  description = "Specifies the minimum number of nodes which should exist within this Node Pool. Valid values are between 0 and 1000 and must be less than or equal to max_count."
  type        = number
  default     = 1
}

variable "agents_max_count" {
  description = "Specifies the maximum number of nodes which should exist within this Node Pool. Valid values are between 0 and 1000 and must be greater than or equal to min_count."
  type        = number
  default     = 5
}

variable "agents_max_pods" {
  description = "Specifies the maximum number of pods that can run on each agent. Changing this forces a new resource to be created."
  type        = number
  default     = 50
}

variable "azure_policy_enabled" {
  description = "Should the Azure Policy Add-On be enabled? For more details please visit Understand Azure Policy for Azure Kubernetes Service"
  type        = bool
  default     = false
}

variable "network_policy" {
  description = "Specifies the type of network policy to use for Kubernetes."
  type        = string
  default     = "azure"
}

variable "microsoft_defender_enabled" {
  description = "Should Microsoft Defender for Containers be enabled? For more details please visit Microsoft Defender for Containers"
  type        = bool
  default     = false
}

variable "net_profile_dns_service_ip" {
  description = "Specifies the DNS service IP"
  default     = "172.20.0.10"
  type        = string
}

variable "net_profile_service_cidr" {
  description = "Specifies the service CIDR"
  default     = "172.20.0.0/16"
  type        = string
}

variable "build_backstage" {
  description = "Flag to control whether Backstage-related components are built"
  type        = bool
  default     = false
}

variable "postgres_password" {
  description = "Password for the Backstage Postgres database, sourced from AKV at apply time via OIDC federation."
  type        = string
  sensitive   = true
}

variable "jenkins_admin_username" {
  description = "Bootstrap admin username to seed in the management Key Vault for Jenkins."
  type        = string
  default     = "platform-admin"
}

variable "jenkins_admin_password" {
  description = "Bootstrap admin password to seed in the management Key Vault for Jenkins. Sourced from AKV at apply time via OIDC federation."
  type        = string
  sensitive   = true
}

variable "jenkins_bitbucket_workspace_token" {
  description = "Bootstrap Bitbucket workspace token to seed in the management Key Vault for Jenkins. Sourced from AKV at apply time via OIDC federation."
  type        = string
  sensitive   = true
}

variable "jenkins_jira_service_account_token" {
  description = "Bootstrap Jira service account token to seed in the management Key Vault for Jenkins. Sourced from AKV at apply time via OIDC federation."
  type        = string
  sensitive   = true
}

variable "jira_base_url" {
  description = "Base URL for the Jira Cloud or Jira Data Center instance used by platform automation."
  type        = string
  default     = "https://example.atlassian.net"
}

variable "jira_project_key" {
  description = "Jira project key where Argo CD incidents and drift events should be created."
  type        = string
  default     = "IDP"
}

variable "jira_service_account_email" {
  description = "Optional Jira service account email used for Basic auth flows against Jira Cloud. Leave empty to use Bearer token auth."
  type        = string
  default     = ""
}

variable "jenkins_bitbucket_server_url" {
  description = "Bitbucket base URL used by the Jenkins multibranch source."
  type        = string
  default     = "https://bitbucket.org"
}

variable "jenkins_bitbucket_repo_owner" {
  description = "Bitbucket workspace or project owner that hosts service repositories scanned by Jenkins."
  type        = string
  default     = "platform-prod"
}

variable "jenkins_service_repository" {
  description = "Bootstrap service repository name for the Jenkins multibranch pipeline."
  type        = string
  default     = "myapp"
}

variable "jenkins_platform_gitops_repo_url" {
  description = "Bitbucket HTTPS URL for the platform-gitops repository updated by Jenkins."
  type        = string
  default     = "https://bitbucket.org/platform-prod/platform-gitops.git"
}

variable "jenkins_webhook_internal_load_balancer_ip" {
  description = "Static private IP assigned to the internal Jenkins load balancer in the mgmt-we AKS subnet."
  type        = string
  default     = "10.1.0.50"
}

variable "jenkins_webhook_https_keystore_base64" {
  description = "Base64-encoded Jenkins HTTPS keystore content used by the controller to terminate TLS for Bitbucket webhook ingress. Sourced from AKV at apply time via OIDC federation."
  type        = string
  sensitive   = true
}

variable "jenkins_webhook_https_keystore_password" {
  description = "Password for the Jenkins HTTPS keystore mounted into the controller pod. Sourced from AKV at apply time via OIDC federation."
  type        = string
  sensitive   = true
}

variable "jenkins_webhook_allowed_ipv4_cidrs" {
  description = "Current Atlassian Bitbucket Cloud IPv4 egress CIDRs allowed to reach the Jenkins webhook endpoint through Front Door and Azure Firewall."
  type        = list(string)
  default = [
    "104.192.136.0/21",
    "185.166.140.0/22",
    "34.218.156.209/32",
    "34.218.168.212/32",
    "52.41.219.63/32",
    "104.192.137.240/28",
    "104.192.143.240/28",
    "18.136.214.96/28",
    "13.236.8.224/28",
    "18.184.99.224/28",
    "185.166.143.240/28",
    "52.215.192.224/28",
    "185.166.142.240/28",
    "18.234.32.224/28",
    "104.192.142.240/28",
    "13.52.5.96/28",
    "18.246.31.224/28",
    "104.192.140.240/28",
    "13.200.41.128/25",
    "104.192.137.0/24",
    "104.192.143.0/24",
    "185.166.143.0/24",
    "185.166.142.0/24",
    "104.192.142.0/24",
    "104.192.140.0/24",
    "3.216.235.48/32",
    "34.231.96.243/32",
    "44.199.3.254/32",
    "174.129.205.191/32",
    "44.199.127.226/32",
    "44.199.45.64/32",
    "3.221.151.112/32",
    "52.205.184.192/32",
    "52.72.137.240/32",
    "34.232.119.183/32",
    "35.155.178.254/32",
    "34.216.18.129/32",
    "35.171.175.212/32",
    "35.160.177.10/32",
    "34.199.54.113/32",
    "52.204.96.37/32",
    "34.232.25.90/32",
    "52.202.195.162/32",
    "52.54.90.98/32",
    "52.203.14.55/32",
    "34.236.25.177/32",
    "34.233.65.54/32",
    "34.196.8.197/32",
    "44.194.7.14/32",
  ]
}

variable "backstage_tls_crt" {
  description = "PEM-encoded TLS certificate for Backstage, sourced from AKV at apply time via OIDC federation. Never stored in tfvars."
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^-----BEGIN CERTIFICATE-----", var.backstage_tls_crt))
    error_message = "backstage_tls_crt must be a PEM-encoded certificate beginning with '-----BEGIN CERTIFICATE-----'."
  }
}

variable "backstage_tls_key" {
  description = "PEM-encoded TLS private key for Backstage, sourced from AKV at apply time via OIDC federation. Never stored in tfvars."
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^-----BEGIN (RSA |EC |PRIVATE KEY)", var.backstage_tls_key))
    error_message = "backstage_tls_key must be a PEM-encoded private key."
  }
}
