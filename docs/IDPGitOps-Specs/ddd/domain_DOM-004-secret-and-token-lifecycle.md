---
title: Domain — Secret and token lifecycle
id: DOM-004
kind: domain
feature: F-004
status: draft
owner: Platform Engineering
updated: 2026-09-21
---

# Domain — Secret and token lifecycle

> Business rules, process and user flow for this domain, in the language of the business.

## Ubiquitous language

| Term | Definition | Source |
| --- | --- | --- |
| Catalogued secret | A secret the platform declares, with a recorded owner of its write path | [D: terraform/locals.tf:41] |
| Terraform-managed | Written by infrastructure code | [D: terraform/locals.tf:38] |
| Referenced-only | Named by infrastructure code, written by the rotator | [D: terraform/locals.tf:64] |
| Vault pair | The two regional vaults a rotated value is written to | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:73] |
| Strategy | What a write does when the target is soft-deleted | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:44] |
| Grace period | The wait between publishing a new version and disabling the old ones | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:71] |
| Secret age | Time since the latest version was written | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:157] |
| Skew | Divergence between the two regional vaults | [D: terraform/akv_sync_exporter.tf:1] |

## Actors

| Actor | Role | Evidence |
| --- | --- | --- |
| Rotator | Mints and writes SaaS credentials on a schedule | [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20] |
| Terraform | Writes the secrets it owns and sets their expiry | [D: terraform/keyvaults.tf:66] |
| Consumer | Reads the projected secret; named in the catalogue | [D: terraform/locals.tf:45] |
| Exporter | Publishes the dual-write skew metric | [D: terraform/akv_sync_exporter.tf:1] |

## Business rules

- **DOM-004-R1** — Every platform secret is catalogued, in exactly one of two modes. [D: terraform/locals.tf:41] [D: terraform/locals.tf:67]
- **DOM-004-R2** — A Terraform-managed secret must also exist as a Terraform resource and in the vault catalogue header. [D: terraform/locals.tf:38]
- **DOM-004-R3** — A rotated credential is written to both regional vaults. A run that writes one and fails on the other does not complete: it aborts [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:131], and the scheduled job's on-failure retry re-runs it, minting a fresh credential and rewriting both [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:23]. The pair is **eventually consistent, not atomic**; divergence beyond ten minutes raises an alert [D: gitops/platform/akv-sync-exporter/templates/prometheus-rule.yaml:18].
- **DOM-004-R4** — All secret writes go through one path; no caller builds its own. [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76]
- **DOM-004-R5** — A default write never revives a soft-deleted secret; recovery is an explicit choice. [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:50]
- **DOM-004-R6** — Failures are typed so a caller branches on cause rather than on message text. [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58]
- **DOM-004-R7** — Every platform-managed secret carries an expiry date from the quarterly boundary. [D: terraform/keyvaults.tf:66]
- **DOM-004-R8** — After a rotation only the newest version stays enabled; superseded versions are disabled, not deleted. [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:308]
- **DOM-004-R9** — No secret value appears in an error, a log or a message; response bodies are redacted by default. [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208]
- **DOM-004-R10** — Rotation happens only on the active management cluster. [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115]
- **DOM-004-R11** — A grace period separates publishing a new version from disabling the old, and it ends immediately on shutdown. [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:140]

## Process flow

Validate, check leadership, mint, write west, write north, wait out the grace
period, disable superseded versions **in both regional vaults**, report age
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:110].

A run that fails part-way is re-run by the scheduled job rather than repaired by
hand; each retry mints a fresh credential and rewrites both vaults
[D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:23].

Errors: a standby cluster does nothing
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:115]; a mint or vault
failure aborts [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:121]; a
disable failure is logged
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:146].

## Invariants

- One write path, one strategy enum, one set of typed errors [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76].
- A catalogued secret always names its consumers [D: terraform/locals.tf:45].
- A secret value never crosses into a log line [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208].

## Implementation status

| Rule | Status | Evidence | Covering test in tree |
| --- | --- | --- | --- |
| DOM-004-R1 | done — python3 scripts/validate-akv-catalogue.py exits 0 here 2026-09-18 | — | — |
| DOM-004-R2 | done — python3 scripts/validate-akv-catalogue.py exits 0 here 2026-09-20 — it enforces R2's round-trip in both directions, 17 entries | — | — |
| DOM-004-R3 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:72] |
| DOM-004-R4 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/akvwriter/fake_test.go:181] |
| DOM-004-R5 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:113] |
| DOM-004-R6 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:407] |
| DOM-004-R7 | done — NULL_EXPIRY_MODE=block python3 scripts/validate-akv-null-expiry.py exits 0 here 2026-09-20 in blocking mode, not only advisory | — | — |
| DOM-004-R8 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:308] |
| DOM-004-R9 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208] |
| DOM-004-R10 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:55] |
| DOM-004-R11 | done — go test ./... in tools/mgmt-plane-lock, every package ok here 2026-09-18 | — | [D: tools/mgmt-plane-lock/internal/rotation/rotation_test.go:114] |

## Open questions

- RESOLVED 2026-09-21: DOM-004-R3 is authoritative and already holds — the wording was imprecise, not the code. A run genuinely does not complete when either write fails; what the rule did not say is what happens to the pair afterwards. The convergence mechanism is the scheduled job's on-failure retry, which re-mints and rewrites both vaults [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:23]; operator repair is the fallback the skew alert exists for, not the design. R3 now says so. Nothing tests the divergent state — F-004-T10 carries that.
- RESOLVED 2026-09-20: DOM-004-R2 is enforced on every change. `scripts/validate-akv-catalogue.py` asserts the round-trip in both directions — every Terraform-declared secret or certificate name appears in the catalogue header, and every catalogue name maps to a real resource [D: scripts/validate-akv-catalogue.py:7] — and CI runs it as the `akv-catalogue` job on any change to `terraform/` or the script itself [D: .github/workflows/terraform-ci.yml:214]. It is not a pre-commit hook.
- RESOLVED 2026-09-20: The grace period is 24 hours in the deployed configuration — `gracePeriodHours: 24` [D: gitops/platform/saas-token-rotator/values.yaml:16], passed to both CronJobs as `--grace-period-hours` [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:28], matching the binary's own flag default [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/main.go:23]. It protects a consumer holding the superseded credential mid-flight, which is F-004-US3.
- OPEN: Who rotates the credentials the rotator itself uses to mint? They are two Kubernetes Secrets read by `secretKeyRef` — `saas-rotator-bitbucket-creds` (workspace, OAuth client id and secret) and `saas-rotator-jira-creds` (service-account email and current API token) [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:36]. Neither is in the vault catalogue, so nothing in this feature rotates them.
- OPEN: DOM-004-R7 holds today, verified in blocking mode, but CI runs the expiry validator with `NULL_EXPIRY_MODE: advisory` [D: .github/workflows/terraform-ci.yml:207], so a future secret can land without an expiry and only warn. When is the cutover to `block`?
