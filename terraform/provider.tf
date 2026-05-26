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

provider "kubernetes" {
  config_path = local_file.kubeconfig.filename

}

provider "helm" {
  kubernetes {
    config_path = local_file.kubeconfig.filename
  }
}
provider "random" {}

provider "time" {}
