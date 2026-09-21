---
title: API Contract v4 F-004 — Secret and token lifecycle
id: F-004
status: draft
owner: Platform Engineering
updated: 2026-09-21
---

# API Contract v4 F-004 — Secret and token lifecycle

> The wire contract for this feature — the readable companion to its OpenAPI and AsyncAPI files.

## Surface summary

Nothing served. One vault write path used by every caller
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76], two SaaS mint calls
[D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20], and a Terraform
catalogue that declares which secrets exist and who writes them
[D: terraform/locals.tf:41].

## Endpoints

| Operation | Purpose | Auth | Evidence |
| --- | --- | --- | --- |
| Put secret | Write a value, strategy-controlled against soft delete | Workload Identity token | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:76] |
| Get secret | Read the projection (name, value, updated-at, enabled) | Workload Identity token | [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:77] |
| Disable old versions | Leave only the latest enabled after a rotation | Workload Identity token | [D: tools/mgmt-plane-lock/internal/rotation/rotation.go:146] |
| Mint source-host token | Obtain a new SaaS credential | SaaS credentials | [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:20] |
| Mint tracker token | Obtain a new SaaS credential | SaaS credentials | [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/minters.go:57] |

Every outbound HTTP call goes through the shared transport seam — 30 s per attempt, 3 retries, 200 ms backoff doubling to a 5 s cap — which retries 5xx
and does not retry 4xx [D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:55]
[D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:81].

## Events

**None as a message contract.** The only event-shaped configuration in the survey is
a Key Vault alert topic type [D: terraform/akv_alerts.tf:47], consumed by Azure
Monitor rather than by this code.

## Error model

Four typed sentinels, tested with `errors.Is`
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58]: not found, auth
failed, conflict, and soft-deleted-but-recoverable. A 5xx returns a redacted error
[D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter_test.go:190], and the
transport error's body is redacted by default
[D: tools/mgmt-plane-lock/internal/httpx/httpx_test.go:208].

Rotation treats failures differently by stage: a mint failure, or either vault
write failing, aborts the run
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:121]; a failure to disable
old versions is logged and not fatal
[D: tools/mgmt-plane-lock/internal/rotation/rotation.go:146].

## Versioning and compatibility

| Stable | Changes carefully |
| --- | --- |
| The four sentinels, matched by callers | Adding one changes caller branching [D: tools/mgmt-plane-lock/internal/akvwriter/akvwriter.go:58] |
| Secret names in the catalogue | Renaming one breaks every consumer named in `consumed_by` [D: terraform/locals.tf:45] |
| The two catalogue modes | Moving an entry between them moves ownership of the write path [D: terraform/locals.tf:64] |

## Spec files

- [`schema/openapi_v4_F-004.json`](schema/openapi_v4_F-004.json) — outbound operations only.
- [`schema/asyncapi_v4_F-004.json`](schema/asyncapi_v4_F-004.json) — empty, stated.

## Traceability

| Surface | Satisfies |
| --- | --- |
| Single write path | F-004-US4 · DOM-004-R4 |
| Strategy against soft delete | F-004-US4 · DOM-004-R5 |
| Dual-vault write | F-004-US2 · DOM-004-R3 |
| Disable old versions | F-004-US3 · DOM-004-R8 |
| Mint operations | F-004-US2 · DOM-004-R3 |
| Catalogue modes | F-004-US1 · DOM-004-R1, R2 |

## Open questions

- RESOLVED 2026-09-21: The pair is eventually consistent, not atomic. A failure between the two writes aborts the run, and the scheduled job's on-failure retry re-mints and rewrites both [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:23]. The skew alert [D: gitops/platform/akv-sync-exporter/templates/prometheus-rule.yaml:18] covers the case where the retry budget is exhausted; operator repair is the fallback, not the design. See DOM-004-R3.
- RESOLVED 2026-09-20: The grace period is 24 hours — `gracePeriodHours: 24` [D: gitops/platform/saas-token-rotator/values.yaml:16] rendered into both CronJobs' `--grace-period-hours` argument [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:28]; the binary's flag default is the same [D: tools/mgmt-plane-lock/cmd/saas-token-rotator/main.go:23].
- OPEN: How are the rotator's own minting credentials rotated? They are the Kubernetes Secrets `saas-rotator-bitbucket-creds` and `saas-rotator-jira-creds` [D: gitops/platform/saas-token-rotator/templates/cronjobs.yaml:36], neither of which is in the vault catalogue.
