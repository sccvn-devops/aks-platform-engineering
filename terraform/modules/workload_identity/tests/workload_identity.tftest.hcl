# Terraform test suite for the workload_identity module (FR-V4-07).
#
# Requires terraform >= 1.6 to run via `terraform test`.  On the project's
# pinned 1.5.x runtime, the equivalent static assertions are enforced by
# tools/workload_identity_tests/test_module_contract.py (see CI job
# `validate-workload-identity`).
#
# These tests use mock_provider "azurerm" so they exercise the module without
# Azure credentials.  Each `command = plan` run-block asserts the values
# computed by the module (subject, audience, scope) match the inputs verbatim.

mock_provider "azurerm" {
  mock_resource "azurerm_user_assigned_identity" {
    defaults = {
      principal_id = "00000000-0000-0000-0000-000000000001"
      client_id    = "00000000-0000-0000-0000-000000000002"
      tenant_id    = "00000000-0000-0000-0000-000000000003"
    }
  }
}

variables {
  name                = "uami-test"
  location            = "westeurope"
  resource_group_name = "rg-test"
  tags                = { service = "test" }
}

run "federated_subject_is_constructed_from_namespace_and_sa" {
  command = plan

  variables {
    federated_credentials = {
      "velero" = {
        issuer                    = "https://oidc.example.invalid/issuer"
        service_account_namespace = "velero"
        service_account_name      = "velero-server"
      }
    }
  }

  assert {
    condition     = azurerm_federated_identity_credential.this["velero"].subject == "system:serviceaccount:velero:velero-server"
    error_message = "FR-V4-09: subject must be system:serviceaccount:<ns>:<sa>."
  }

  assert {
    condition     = azurerm_federated_identity_credential.this["velero"].audience[0] == "api://AzureADTokenExchange"
    error_message = "audience must be api://AzureADTokenExchange."
  }
}

run "subscription_scope_is_rejected_by_default" {
  command = plan

  variables {
    role_assignments = {
      "broad" = {
        scope                = "/subscriptions/11111111-1111-1111-1111-111111111111"
        role_definition_name = "Contributor"
      }
    }
  }

  expect_failures = [
    azurerm_role_assignment.this["broad"],
  ]
}

run "subscription_scope_allowed_when_explicit" {
  command = plan

  variables {
    allow_subscription_scope = true
    role_assignments = {
      "broad" = {
        scope                = "/subscriptions/11111111-1111-1111-1111-111111111111"
        role_definition_name = "Contributor"
      }
    }
  }

  assert {
    condition     = azurerm_role_assignment.this["broad"].scope == "/subscriptions/11111111-1111-1111-1111-111111111111"
    error_message = "scope must equal the input scope verbatim."
  }
}

run "rg_scope_passes_default_guard" {
  command = plan

  variables {
    role_assignments = {
      "rg" = {
        scope                = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-test"
        role_definition_name = "Reader"
      }
    }
  }

  assert {
    condition     = azurerm_role_assignment.this["rg"].role_definition_name == "Reader"
    error_message = "RG-scope grants must pass the default subscription-scope guard."
  }
}
