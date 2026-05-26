# Cross-variable invariant preconditions — FR-V4-42, US-V4-10 (Robustness pass).
#
# Terraform allows resources to declare per-attribute or whole-resource
# preconditions inside lifecycle blocks.  This file consolidates the
# invariants that must hold ACROSS the cluster registry and the network
# topology *before* any resource is evaluated — terraform_data carries no
# provider work, so a failed precondition aborts the plan up-front with the
# named invariant in the error message.
#
# Invariants enforced here:
#
#   1. CIDR non-overlap (FR-V4-42a):  every spoke VNet address space is
#      disjoint from every hub VNet address space.  Catches accidental
#      mis-typed CIDRs that would route traffic in unexpected ways.
#
#   2. Registry region_abbrev canonicalisation (FR-V4-42b):  every
#      cluster_registry entry's `region_abbrev` matches the canonical
#      short code for its `region`.  Catches a registry edit that bumps
#      `region` to a new Azure region without updating `region_abbrev`,
#      which would otherwise silently break ACR hostnames and DNS suffix
#      assumptions downstream.
#
# Why terraform_data, not `check` blocks: `check` blocks emit advisory
# warnings; preconditions on terraform_data resources fail the plan loudly
# at evaluation time, matching the FR-V4-42 wording ("the plan fails with
# the named invariant before any resource is evaluated").

locals {
  # Canonical Azure region → short code map.  Add new entries here when a
  # cluster registers in a new region; the registry's region_abbrev must
  # match the value declared here.
  region_abbrev_canonical = {
    westeurope  = "we"
    northeurope = "ne"
    westus2     = "wus"
  }

  # First IP of each hub address space, used for CIDR-overlap detection.
  # cidrhost(<cidr>, 0) returns the network address; for /16 CIDRs (the
  # current network model) this equals the network identifier and is
  # sufficient to detect duplicate / overlapping ranges of the same prefix
  # length.  If we ever mix prefix lengths between hub and spoke, this
  # check must be extended to compare both prefixes — see comment on the
  # network_invariants resource below.
  hub_network_addresses = {
    for k, h in local.hub_networks : k => cidrhost(h.address_space[0], 0)
  }
}

# Network-topology invariants: one terraform_data resource per spoke so the
# error message names the offending spoke key without listing every spoke.
resource "terraform_data" "network_invariants" {
  for_each = local.spoke_networks

  input = {
    spoke = each.key
    cidr  = each.value.address_space[0]
  }

  lifecycle {
    precondition {
      # FR-V4-42a: spoke CIDR must not equal any hub CIDR.  All current
      # CIDRs are /16; comparing cidrhost(_, 0) catches any direct overlap
      # for equal-prefix-length cases.  Mixed-prefix overlap detection is
      # documented as a future extension.
      condition = !contains(
        values(local.hub_network_addresses),
        cidrhost(each.value.address_space[0], 0)
      )
      error_message = format(
        "FR-V4-42a CIDR overlap: spoke '%s' address space %s collides with a hub CIDR (network address %s). Spoke and hub CIDRs must be disjoint.",
        each.key,
        each.value.address_space[0],
        cidrhost(each.value.address_space[0], 0)
      )
    }
  }
}

# Registry-topology invariants: one terraform_data resource per cluster so
# the error message names the offending entry.  Runs against the loaded
# registry (registry.tf) and fails the plan before any Azure resource is
# evaluated.
resource "terraform_data" "registry_invariants" {
  for_each = local.cluster_registry

  input = {
    cluster       = each.key
    region        = each.value.region
    region_abbrev = each.value.region_abbrev
  }

  lifecycle {
    precondition {
      # FR-V4-42b: registry region_abbrev must match the canonical short
      # code for the declared region.  Missing entries in
      # region_abbrev_canonical also fail (lookup returns null → not equal
      # to the registry value), which forces the operator to update the
      # canonical map alongside the registry.
      condition = (
        lookup(local.region_abbrev_canonical, each.value.region, null)
        == each.value.region_abbrev
      )
      error_message = format(
        "FR-V4-42b registry inconsistency: cluster '%s' has region='%s' but region_abbrev='%s'; canonical short code is '%s'. Update gitops/clusters/registry.yaml or extend local.region_abbrev_canonical in terraform/invariants.tf.",
        each.key,
        each.value.region,
        each.value.region_abbrev,
        lookup(local.region_abbrev_canonical, each.value.region, "<unknown region — add to canonical map>")
      )
    }
  }
}
