---
title: Research — IDPGitOps
status: draft
owner: Platform Engineering
updated: 2026-09-18
---

# Research — IDPGitOps

> External APIs, third-party specs and spikes, with the date each was checked.

"Checked" means verified against this repository's code on that date, not against a
vendor's documentation.

## Third-party integrations

| Service | Purpose | Auth in code | Bounds | Checked |
| --- | --- | --- | --- | --- |
| Issue tracker REST v3 | Read a service request; open drift tickets | Basic with email, Bearer without [D: tools/service_seed/jira_intake.py:214] | 30 s [D: tools/service_seed/jira_intake.py:27] | 2026-09-18 |
| Source host REST 2.0 | Create a repository; open change sets | Basic [D: tools/service_seed/gitops_pr.py:86] | 30 s; 300 s for git [D: tools/service_seed/gitops_pr.py:34] | 2026-09-18 |
| Azure Storage blob lease | Arbitrate the active management cluster | SDK client [D: tools/mgmt-plane-lock/internal/bloblease/bloblease.go:43] | 15–60 s term [D: tools/mgmt-plane-lock/internal/config/config.go:79] | 2026-09-18 |
| Azure Key Vault | Secret material | SDK client [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:81] | Retry policy in the transport seam [D: tools/mgmt-plane-lock/internal/httpx/httpx.go:1] | 2026-09-18 |
| Azure Resource Manager | Everything Terraform provisions | Provider configuration [D: terraform/provider.tf:1] | — | 2026-09-18 |
| Kubernetes API | Status object, replica patches, cluster Secret label | In-cluster client [D: tools/mgmt-plane-lock/internal/bootstrap/kube.go:1] | — | 2026-09-18 |
| GitHub Actions | Platform CI | Repository-scoped [D: .github/workflows/terraform-ci.yml:1] | Actions SHA-pinned [D: scripts/validate-action-pins.py:72] | 2026-09-18 |

## Spikes

OPEN: No spike record exists in the repository. Where a decision was clearly
weighed — lease versus label, zero-replica standby versus not installing, committed
registry versus a CRD — the reasoning lives in the repository's ADR set
[D: _docs/IDP-GitOps-ADRs-v2.md:1] and is cited rather than restated here.

Questions this reverse pass raised, none answered in the tree:

- Why does a healthy holder release on a preference change [D: tools/mgmt-plane-lock/internal/bloblease/runner.go:146]?
- Why is `silver` the default tier [D: tools/service_seed/jira_intake.py:159]?
- Why do the two production cluster keys appear as constants in the renderer [D: tools/service_seed/service_template.py:46]?
- What is the grace period meant to protect [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:71]?

## Reference material

| Reference | Why |
| --- | --- |
| `docs/architect.md` | The repository's own engineering guide; the richest prose source, and the one that diverges from code in two places this hub records |
| `_docs/IDP-GitOps-ADRs-v2.md` | The decision record; authoritative for why |
| `_docs/IDP-GitOps-Blueprint-PRD-v4.md` | The functional contract the code was built against |
| `docs/agents/domain.md` | The settled decisions an agent must not silently reopen |
| `walkthrough.md` | A generated code walkthrough, useful for orientation |
| `gitops/clusters/registry.schema.json` | The committed schema behind the catalogue |
