# PRD: IDP GitOps Blueprint v4 — Platform Code Architecture Deepening

**Version:** 4.0
**Date:** 2026-05-25
**Author:** Platform Engineering
**Status:** Approved 2026-05-26 — ADR-031-v4 Accepted alongside this PRD

---

## Purpose

This PRD is an **additive addendum** to `IDP-GitOps-Blueprint-PRD.md` (v2) and
`IDP-GitOps-Blueprint-PRD-v3.md` (Terraform hardening). It extends — and does
not modify — the v2/v3 scope by deepening shallow modules and turning
implicit seams into explicit, tested ones across the platform code (Terraform,
Go tooling, Python tooling, and CI workflows). All v2 functional requirements
(FR-1..FR-25, US-001..US-025) and v3 requirements (FR-V3-NN, US-V3-NN,
OQ-V3-NN) remain in force verbatim. PRD-v4 adds new requirements under a
separate `FR-V4-NN` / `US-V4-NN` / `OQ-V4-NN` namespace so the v2/v3 numbering
is preserved.

The v4 theme is **architecture deepening + hardening pass**: convert the
three-way encoding of cluster topology, the copy-pasted workload-identity
pattern, the two duplicate Terraform CI workflows, the open-coded Go HTTP
policies, the 981-line `seed_job.py` god module, and the hand-rolled YAML
emission into modules with high leverage and good locality — and close the
residual security/robustness gaps the deepening exposes.

---

## Pending Approvals

PRD-v4 shipped with **one new ADR** that was approved alongside this PRD:

- **ADR-031-v4 — Cluster topology lives in a single committed registry**
  (referenced by FR-V4-01..04). Status: **Accepted (PRD-v4 approved
  2026-05-26)**. This section is retained as a historical record of the
  co-approval gate; the gate has been satisfied.

No other v2/v3 ADR is modified by this PRD.

---

## Scope

### In Scope (v4 additions only)

| Area | Addition |
|---|---|
| Cluster topology | Single committed YAML registry at `gitops/clusters/registry.yaml`; consumed by Terraform (`yamldecode`), ArgoCD ApplicationSet locals, and `tools/service_seed/`; codified by ADR-031-v4 |
| Terraform module depth | New `terraform/modules/workload_identity/` collapses the UAMI + federated-credential + role-assignment pattern repeated across Jenkins, ESO, AKV-sync, saas-token-rotator, Velero |
| CI/CD shape | Two reusable workflows (`terraform-plan.yml`, `terraform-apply.yml` with `workflow_call:`) + composite action `./.github/actions/setup-tf`; apply uses two-phase matrix (mgmt-first, workloads-after) |
| Go HTTP policy | New `tools/mgmt-plane-lock/internal/httpx/` exposes a transport-only seam (timeouts, retry, redacted errors); consumed by `akvwriter`, `saas-token-rotator`, `argocd-jira-bridge` |
| Secret writer | `internal/akvwriter` becomes the single AKV write path; `saas-token-rotator` stops open-coding AKV calls; consumer-defined narrow interfaces for tests |
| Go binary lifecycle | New `internal/bootstrap/` for SIGTERM-aware context + metrics server; per-binary `Runner` types lifted out of `main` into existing `internal/scaling`, `internal/bloblease` |
| Python tooling | `seed_job.py` (981 lines) splits into `jira_intake.py`, `service_template.py`, `gitops_pr.py`, plus a thin `cli.py`; gains `pyproject.toml` |
| Template emission | Jinja2 templates under `tools/service_seed/templates/`; SLO + rollout numerics in `tools/service_seed/profiles/{slo,rollout}.yaml`; rendered output validated by `kubeconform` in CI |
| Secret transport | Hybrid Helm→ESO migration: system-generated secrets (random_password) flow TF→AKV→ESO; operator-rotated secrets flow operator→AKV→ESO; no `helm_release.set { value = sensitive_var }` survives |
| Robustness | `lifecycle { prevent_destroy = true }` on state-lease + Velero containers; `precondition{}` blocks for cross-variable invariants; `timeout=` on all Python `urlopen`/subprocess calls |
| CI hygiene | All GitHub Actions pinned to commit SHAs (Dependabot-managed); `.tool-versions` (asdf/mise) is the sole source of truth for TF/tflint/checkov/terragrunt; Checkov baseline gains per-finding `rationale|owner|expiry`; pre-commit Checkov scope matches CI |

### Out of Scope (v4)

See **§Non-Goals** for the full three-bucket breakdown.

---

## Background

A code-architecture review of the v3 codebase (post-merge of branch
`feat/prd-v3-strengthen-tf-code-quality`) surfaced **18 deepening
opportunities** — places where shallow modules with thin interfaces had
accreted across Terraform, Go, Python, and CI workflows. The findings split
into two tiers:

| Tier | Count | Theme |
|---|---|---|
| **Deepening (T1)** | 8 | Real seams that exist as one-or-two adapters today but lack a named module — copy-paste, three-way encoded topology, god module, etc. The deletion test passes for each: removing the proposed module would re-fan-out identical complexity across 3+ call sites. |
| **Hardening (T2)** | 10 | Focused security/robustness/CI-hygiene gaps. Each is a one-to-three-line fix individually but they share rollout shape with T1. |

PRD-v4 ships all 18 as user stories. T1 items each become a full user story
with its own design surface (US-V4-01..08). T2 items collapse into three
bundled stories grouped by axis (US-V4-09 QW-SEC, US-V4-10 QW-ROB, US-V4-11
QW-CI) to keep narrative density tight.

The v3 PRD assumed the existing module decomposition was load-bearing; the
review proved that ~40% of v3-era `internal/`, `terraform/*.tf`, and CI
workflow surface is shallow — pass-through modules where the interface is
nearly as complex as the implementation. v4 does not modify v3's behaviors;
it deepens the seams that v3 left exposed.

**Question-resolution namespace.** During the PRD-v4 grilling session, 16
sequential design questions were resolved. Each is referenced in this
document as `Q-V4-NN` (grilling question NN). This namespace is distinct
from `OQ-V4-NN` (Open Questions, listed in §Open Questions). Grilling
questions are *resolved*; Open Questions are *deliberately deferred*.
ADR-031-v4 resolves `Q-V4-03`.

---

## Functional Requirements

### Cluster Topology Registry (item #7 — requires ADR-031-v4)

- **FR-V4-01** — A single YAML registry committed at
  `gitops/clusters/registry.yaml` must be the sole authoritative source for
  cluster topology. Day-1 keys: `mgmt-we`, `mgmt-ne`, `aks-dev-we`,
  `aks-staging-we`, `aks-prod-we`, `aks-prod-ne`, `seed-wus`. Each entry
  carries: `subscription_id`, `region`, `region_abbrev`, `resource_group`,
  `acr_hostname`, `aks_name`, `mgmt_role` (`active|standby|workload|seed`),
  `azs` (list), `sku_tier`, `gitops_addons` (map of `enable_*` flags).
- **FR-V4-02** — Terraform locals must read the registry via
  `yamldecode(file("${path.module}/../gitops/clusters/registry.yaml"))`;
  no cluster identity may be hardcoded in `terraform/*.tf` after v4.
- **FR-V4-03** — `tools/service_seed/` must read the same file; no
  subscription IDs, regions, RG names, or ACR hostnames may be hardcoded in
  `seed_job.py` or its replacements after v4.
- **FR-V4-04** — A JSON Schema at `gitops/clusters/registry.schema.json`
  must validate the registry in pre-commit and in CI; a pre-commit hook
  rejects a PR that breaks the schema. ArgoCD ApplicationSet locals derive
  from the registry without hand maintenance.

### Workload Identity Terraform Module (item #4)

- **FR-V4-05** — A reusable Terraform module must exist at
  `terraform/modules/workload_identity/` with a **single-identity**
  interface. Inputs: `name`, `resource_group`, `location`,
  `kubernetes_namespace`, `kubernetes_service_account`, `oidc_issuer_url`,
  `role_assignments` (list of `{scope, role_definition_name}`), `tags`.
- **FR-V4-06** — The module must own only: the `azurerm_user_assigned_identity`,
  the `azurerm_federated_identity_credential` (with audience/issuer/subject
  formatted inside the module), and the role assignments. It must **not**
  own: Key Vault access policies, Helm release wiring, or secret material.
- **FR-V4-07** — Existing identity declarations in `jenkins.tf`,
  `external_secrets.tf`, `akv_sync_exporter.tf`, `saas_token_rotator.tf`,
  and `velero.tf` must migrate to call the module. Callers use HCL
  `for_each` at the call site if multiplicity is needed; the module does
  not accept maps.
- **FR-V4-08** — The duplicated Key Vault role-assignment loops in
  `keyvaults.tf:64–78` and `jenkins.tf:64–68` must collapse into a single
  loop driven by the role-assignment outputs of the module.
- **FR-V4-09** — The module must include `terraform test` blocks asserting:
  a federated credential subject of the form
  `system:serviceaccount:<ns>:<sa>`; role assignments at the requested
  scope only; no role assignment at subscription scope unless explicitly
  marked (`allow_subscription_scope = true`, default `false`).

### Reusable Terraform CI/CD Workflows (item #5)

- **FR-V4-10** — A reusable workflow at
  `.github/workflows/reusable/terraform-plan.yml` with `on: workflow_call:`
  and `permissions: { id-token: write, contents: read, pull-requests:
  write }` must own all `terraform plan` choreography (init, fmt-check,
  validate, tflint, checkov, plan, JSON-diff render, PR comment with 60KB
  truncation). The existing top-level `.github/workflows/terraform-ci.yml`
  is rewritten as a thin caller and invokes it on `pull_request`. No third
  workflow file (e.g., `terraform-apply-main.yml`) is introduced; the
  existing top-level `terraform-ci.yml` and `terraform-apply.yml` are
  retained as the callers. Workflows live at
  `.github/workflows/reusable/terraform-plan.yml` and
  `.github/workflows/reusable/terraform-apply.yml`; the top-level
  `terraform-ci.yml` and `terraform-apply.yml` are rewritten as thin
  callers.
- **FR-V4-11** — A reusable workflow at
  `.github/workflows/reusable/terraform-apply.yml` with `on:
  workflow_call:` and `permissions: { id-token: write, contents: read }`
  (no `pull-requests`) must own all `terraform apply` choreography. The
  existing top-level `.github/workflows/terraform-apply.yml` is rewritten
  as a thin caller and uses a two-phase matrix:
  - Phase 1: `[mgmt-we, mgmt-ne]`
  - Phase 2: `[dev, staging, prod-we, prod-ne, seed-wus]` with
    `needs: [phase-1]`.
  Workflows live at `.github/workflows/reusable/terraform-plan.yml` and
  `.github/workflows/reusable/terraform-apply.yml`; the top-level
  `terraform-ci.yml` and `terraform-apply.yml` are rewritten as thin
  callers.
- **FR-V4-12** — A composite action at `.github/actions/setup-tf/action.yml`
  must own the shared bootstrap: install TF version from `.tool-versions`,
  Azure OIDC login, set TF env (`TF_IN_AUTOMATION`, `TF_INPUT=false`).
  Both reusable workflows consume it.
- **FR-V4-13** — Plan-output truncation logic must exist in exactly one
  place (inside the plan reusable workflow), at one threshold (60KB).
- **FR-V4-14** — GitHub environment protection must apply per matrix entry
  via `environment: ${{ matrix.env }}`. The seven existing environments
  remain unchanged.

### Go HTTP Transport Seam (item #6)

- **FR-V4-15** — A new package `tools/mgmt-plane-lock/internal/httpx/`
  must expose `NewTransport(opts Options) http.RoundTripper`. `Options`
  declares: timeout, retry policy (max attempts, backoff with jitter,
  retry-on set covering 5xx + 429 + network errors), and a redaction policy
  for response bodies in errors.
- **FR-V4-16** — All HTTP-bearing call sites in
  `tools/mgmt-plane-lock/cmd/argocd-jira-bridge/` (the Go binary, not
  the Helm chart at `gitops/platform/argocd-jira-bridge/`),
  `tools/mgmt-plane-lock/cmd/saas-token-rotator/`, and
  `tools/mgmt-plane-lock/internal/akvwriter/` must construct their
  `http.Client` using `httpx.NewTransport(...)`; no direct use of
  `http.DefaultClient` is permitted in those packages.
- **FR-V4-17** — Every `httpx`-mediated call must carry a
  `context.Context`. The package must not accept a nil context. The
  package must provide a context-aware sleep helper used in place of the
  three existing blocking `time.Sleep(...)` calls during shutdown grace
  periods.
- **FR-V4-18** — Error values returned from `httpx` must never embed raw
  response bodies. Callers opt in to body inclusion via an explicit
  `httpx.WithBodyOnError(maxBytes)` option, which still passes the body
  through a configurable redactor.

### AKV Secret Writer Consolidation (item #1)

- **FR-V4-19** — `internal/akvwriter` must be the single AKV write path
  across the binary set. `cmd/saas-token-rotator/main.go:164, 204` must
  stop open-coding AKV PUT calls and consume `akvwriter` instead.
- **FR-V4-20** — `akvwriter` must construct its `http.Client` via
  `httpx.NewTransport(...)` (FR-V4-15) — no bespoke retry or timeout
  remains inside the package.
- **FR-V4-21** — `akvwriter` must return typed errors —
  `ErrSecretNotFound`, `ErrAuthFailed`, `ErrConflict`,
  `ErrSoftDeletedSecretExists` — not generic `error` wrapping HTTP body
  strings.
- **FR-V4-22** — `akvwriter.Put` must accept an explicit `Strategy` enum
  (`Overwrite`, `NewVersionOnly`, `RecoverIfSoftDeleted`); the
  saas-token-rotator's rotation semantic is expressed via strategy, not
  duplicated. An in-memory fake adapter must live in
  `akvwriter/fake_test.go` (test-only export) and back the unit tests.

### Go Binary Lifecycle (item #8)

- **FR-V4-23** — A new package `tools/mgmt-plane-lock/internal/bootstrap/`
  must own SIGTERM/SIGINT-aware context creation (`bootstrap.SignalContext()
  context.Context`) and the metrics HTTP server pattern
  (`bootstrap.ServeMetrics(ctx, addr, registry)`). The package must not own
  flag parsing, configuration loading, or DI wiring.
- **FR-V4-24** — Run-loops in `controller-scaler/main.go:57–73`,
  `mgmt-leader-lease/main.go:71–100`, and `saas-token-rotator/main.go`
  must lift into `Runner` types within their existing `internal/scaling`,
  `internal/bloblease`, and a **new `internal/rotation/`** package
  respectively. Each runner type (`scaling.Runner`,
  `bloblease.LeaseRunner`, `rotation.Runner` — three runners total)
  exposes a single `Run(ctx context.Context) error` method; no shared
  `Runner` interface is introduced. The `argocd-jira-bridge` binary
  takes its lifecycle from `internal/bootstrap` only and does not get a
  new runner.
- **FR-V4-25** — `cmd/saas-token-rotator/main.go:105`'s blocking
  `time.Sleep(...)` during the rotation grace period must convert to a
  `select { case <-time.After(...): case <-ctx.Done(): }` pattern so
  shutdown is honored.
- **FR-V4-26** — Each `main.go` across the four binaries
  (`controller-scaler`, `mgmt-leader-lease`, `saas-token-rotator`,
  `argocd-jira-bridge`) must compress to ≤40 lines after the refactor:
  flag parsing, config load, runner construction (or, for
  `argocd-jira-bridge`, direct bootstrap wiring), single
  `runner.Run(ctx)` call. Coverage targets do not apply to `main.go`.

### service_seed Three-Module Split (item #2)

- **FR-V4-27** — `tools/service_seed/seed_job.py` (981 lines) must split
  into three modules + thin CLI:
  - `tools/service_seed/jira_intake.py` — Jira fetch → `ServiceRequest`
    dataclass; pure parse/validate; no I/O after the Jira fetch.
  - `tools/service_seed/service_template.py` — input: `ServiceRequest`
    + cluster registry data; output: rendered working tree on disk
    (filesystem only).
  - `tools/service_seed/gitops_pr.py` — input: working tree; output:
    pushed Bitbucket branch. Owns `BitbucketClient` and git subprocess.
  - `tools/service_seed/cli.py` — ≤40 lines: argparse, env-var load,
    cluster-registry load, wire three modules.
- **FR-V4-28** — `ServiceRequest` is a frozen `@dataclass` whose
  `__post_init__` validates SLO class, service name, owner team, and
  required fields. It is the only cross-module shared type.
- **FR-V4-29** — Cluster registry data (loaded by CLI from
  `gitops/clusters/registry.yaml` per FR-V4-01) is passed into
  `service_template.render(...)` and `gitops_pr.compose(...)` as data.
  Neither module re-reads the registry.
- **FR-V4-30** — `BitbucketClient` lives inside `gitops_pr.py`, the only
  consumer. Tests use a consumer-defined `RemoteRepo` protocol mocked at
  the call site. The client must not be promoted to a shared module
  until a second consumer exists.
- **FR-V4-31** — A `tools/service_seed/pyproject.toml` must declare the
  package and pin dependencies (jinja2, pyyaml, cookiecutter). The
  package is installable via `pip install -e tools/service_seed`.

### Jinja Templates + SLO/Rollout Profiles (item #3)

- **FR-V4-32** — All YAML emission inside `service_template.py` must be
  template-driven. Templates live under
  `tools/service_seed/templates/{infra,workload/base,workload/overlays}/*.yaml.j2`
  with directory layout mirroring rendered output paths.
- **FR-V4-33** — SLO numerics (success rate, p99 latency, probe interval)
  must live in `tools/service_seed/profiles/slo.yaml`, keyed by SLO class
  (`gold|silver|bronze`). Rollout canary step strategies must live in
  `tools/service_seed/profiles/rollout.yaml`, keyed by SLO class. No SLO
  or rollout literals may remain inside `service_template.py`.
- **FR-V4-34** — Jinja2 must run with `StrictUndefined`. Missing template
  variables fail rendering loudly.
- **FR-V4-35** — A CI step must `kubeconform` rendered output for a
  ServiceRequest fixture of each SLO class against the current Kubernetes
  + CRD schemas. Schema drift fails CI.

### Helm → ESO Migration for Sensitive Values (QW-SEC item Q1)

- **FR-V4-36** — A two-mode helper local must exist in `terraform/locals.tf`
  (or equivalent): `secrets_managed_in_tf` (TF-generated via `random_password`
  → written to AKV → consumed via ESO) and `secrets_referenced_only` (TF
  reads via `data "azurerm_key_vault_secret"`, never generates). Each
  current `helm_release.set { value = sensitive_var }` call site is
  classified under one mode by FR-V4-39.
- **FR-V4-37** — No new `helm_release.set { name = X, value =
  <sensitive_expression> }` may be added after v4 begins. CI enforces via
  a custom tflint rule that fails when `value` references any variable
  declared `sensitive = true`.
- **FR-V4-38** — Every Helm chart consumed by `helm_release` must reference
  secret material through `ExternalSecret`-projected Kubernetes Secrets,
  not Helm values. Charts that do not support this must add the indirection
  before migration.
- **FR-V4-39** — The PRD ships with a registered list of existing call
  sites (see §Appendix A — Helm Set-Value Inventory) and their assigned
  modes; v4 stories migrate them iteratively across P5.
- **FR-V4-40** — Item Q7 (cred leak via response bodies in error strings)
  falls out for free once FR-V4-18 (httpx redaction-by-default) lands; no
  separate FR is needed beyond enforcement.

### Robustness Pass (QW-ROB items Q2, Q6, Q8, Q9)

- **FR-V4-41** — `terraform/storage.tf` (management-plane lease blob
  container) and `terraform/velero.tf` (Velero backup container) must
  carry `lifecycle { prevent_destroy = true }` blocks. Removal requires
  a deliberate two-PR sequence (lift the lifecycle, then destroy).
- **FR-V4-42** — Cross-variable invariants must be expressed as
  `precondition{}` blocks on the consuming resources (not as `check{}`
  advisory blocks). Required day-1 invariants: spoke CIDR
  non-overlap with hub CIDR (`networking.tf`); region/abbrev
  consistency (`region_abbrev` matches `region` per registry);
  Bitbucket CIDR list provenance comment + last-verified date in
  `variables.tf` per FR-V4-44.
- **FR-V4-43** — All Python network and subprocess calls must declare
  explicit timeouts: `urlopen(..., timeout=30)` for HTTP; `subprocess.run(...,
  timeout=300)` for git operations; identical defaults in
  `jira_intake.py`, `gitops_pr.py`, and `service_template.py`. Token
  material must not appear in git remote URLs (use credential helpers
  or `.netrc`).
- **FR-V4-44** — `service_template.py` must validate that no cookiecutter
  template path traverses outside the destination directory. The
  fallback renderer must `assert target.resolve().is_relative_to(destination.resolve())`
  before writing each file. (Closes Q9.)

### CI Hygiene (QW-CI items Q3, Q4, Q5, Q10)

- **FR-V4-45** — All `uses:` references in `.github/workflows/*.yml` and
  `.github/actions/*/action.yml` must pin to a 40-character commit SHA
  with the human-readable tag in a comment. Dependabot configuration must
  open a weekly PR upgrading pinned SHAs.
- **FR-V4-46** — A single `.tool-versions` file at repository root must
  declare versions for `terraform`, `tflint`, `checkov`, `terragrunt`,
  `kubectl`, `kustomize`. CI workflows must source versions from this
  file via `asdf-vm/actions/install@<sha>` or `mise-versions` equivalent.
  Pre-commit hooks must read the same file (via `additional_dependencies`
  templating or a small bootstrap script).
- **FR-V4-47** — `.checkov.yaml` and `.checkov.baseline` must enforce a
  per-finding metadata schema: `rationale` (string, non-empty),
  `owner` (GitHub team or @-handle), `expiry` (ISO date ≤ 180 days from
  baseline entry creation). Findings without all three fields fail CI.
  CODEOWNERS protection on `.checkov.baseline` is retained.
- **FR-V4-48** — Pre-commit Checkov scope must match CI Checkov scope.
  Both must scan `terraform/`, `backstage/**/Dockerfile*`, and any
  Kubernetes manifest under `gitops/` that is hand-authored (templates
  excluded).
- **FR-V4-49** — `.pre-commit-config.yaml`'s tflint hook must pass
  `--config=.tflint.hcl` explicitly (matching CI). Drift between
  pre-commit and CI behavior must be regression-tested by a CI job that
  runs `pre-commit run --all-files` and asserts zero diff after a clean
  CI run.

---

## User Stories

All user stories use **Given / When / Then** acceptance criteria. Each story
inherits the testing floor from §Testing Strategy (80% line coverage on
touched Go `internal/*`; 75% on Python `service_seed/*`; `terraform test`
mandatory for the workload-identity module; `kubeconform` on rendered
templates).

### US-V4-01: Cluster topology registry as data (FR-V4-01..04)

**Description:** As a platform engineer, I want cluster topology defined in
one committed YAML file, so that adding a cluster is a single PR rather than
three coordinated edits across Terraform, ArgoCD locals, and Python.

**Acceptance Criteria:**
- **Given** `gitops/clusters/registry.yaml` contains the 7 day-1 cluster
  entries with required fields,
  **When** `terraform plan` runs in any environment,
  **Then** every cluster identity (subscription_id, region, RG, ACR hostname,
  AKS name) referenced by `terraform/*.tf` resolves via
  `yamldecode(file(...))` and zero literal cluster IDs remain in `terraform/*.tf`.
- **Given** the registry is loaded by `tools/service_seed/cli.py`,
  **When** the CLI seeds a new service for `aks-prod-we`,
  **Then** subscription IDs, regions, RG names, and ACR hostname come from
  the registry and zero hardcoded values appear in `seed_job.py` or its
  replacement modules.
- **Given** `gitops/clusters/registry.schema.json` exists,
  **When** a PR edits the registry with a missing required field or an
  invalid `mgmt_role`,
  **Then** the pre-commit hook fails locally and the CI schema-validation
  job fails the PR.
- **Given** ADR-031-v4 is merged alongside this PRD,
  **When** a reviewer asks "why YAML and not CRD?",
  **Then** ADR-031-v4 is the linked authoritative answer.

### US-V4-02: Workload-identity Terraform module (FR-V4-05..09)

**Description:** As a platform engineer, I want a single
`workload_identity` module owning UAMI + federated credential + role
assignments, so that adding a workload identity is ~10 lines instead of ~30
and audit happens in one place.

**Acceptance Criteria:**
- **Given** `terraform/modules/workload_identity/` exists with the inputs
  declared in FR-V4-05,
  **When** Jenkins, ESO, AKV-sync, saas-token-rotator, and Velero are
  reapplied,
  **Then** all five call sites consume the module and the federated-credential
  audience/issuer/subject construction lives only inside the module.
- **Given** the duplicated KV role-assignment loops in `keyvaults.tf:64–78`
  and `jenkins.tf:64–68` are migrated,
  **When** `terraform plan` runs,
  **Then** the same role assignments are produced from a single loop driven
  by the module's outputs.
- **Given** `terraform test` blocks live in
  `terraform/modules/workload_identity/tests/`,
  **When** the CI test job runs,
  **Then** the tests pass and assert: federated-credential subject is
  `system:serviceaccount:<ns>:<sa>`; no role assignment at subscription scope
  unless `allow_subscription_scope = true`; role-assignment scope matches
  the requested input.
- **Given** a new workload identity needs to be added,
  **When** the engineer opens the PR,
  **Then** the PR diff is ≥60% smaller than the equivalent v3-era PR
  (success-metric verification).

### US-V4-03: Reusable plan/apply workflows + composite action (FR-V4-10..14)

**Description:** As a platform engineer, I want plan and apply logic to
live in two reusable workflows + one composite action, so that adding a
cluster requires one matrix entry instead of a 33-line copy-pasted job.

**Acceptance Criteria:**
- **Given** `.github/workflows/terraform-plan.yml` and
  `.github/workflows/terraform-apply.yml` declare
  `on: workflow_call:` with distinct permission blocks,
  **When** `terraform-ci.yml` runs on a PR,
  **Then** it calls the plan workflow with the PR's environment matrix and
  no inline plan steps remain in `terraform-ci.yml`.
- **Given** `terraform-apply.yml`'s top-level workflow declares a two-phase
  matrix (`mgmt-we`, `mgmt-ne` in phase 1; `dev`, `staging`, `prod-we`,
  `prod-ne`, `seed-wus` in phase 2 with `needs: [phase-1]`),
  **When** a push to `main` triggers apply,
  **Then** phase 2 jobs do not start until phase 1 succeeds and each
  matrix entry has its own GH environment protection check.
- **Given** `.github/actions/setup-tf/action.yml` exists,
  **When** either reusable workflow runs,
  **Then** TF install version, Azure OIDC login, and TF env vars are set
  by the composite action and not duplicated inline.
- **Given** plan output exceeds 60KB,
  **When** the plan job posts the PR comment,
  **Then** the comment is truncated at 60KB with the link to the full
  artifact, and the truncation logic exists in exactly one file.

### US-V4-04: Go HTTP transport seam (FR-V4-15..18)

**Description:** As a Go developer, I want one transport package owning
timeouts/retries/redaction, so that no binary open-codes HTTP policy and
no response body leaks into an error message.

**Acceptance Criteria:**
- **Given** `internal/httpx/` exposes `NewTransport(opts) http.RoundTripper`
  and a redacted error type,
  **When** `argocd-jira-bridge`, `saas-token-rotator`, and `akvwriter` are
  reviewed for HTTP usage,
  **Then** zero `http.DefaultClient` references remain in those packages
  and every `http.Client` is constructed from `httpx.NewTransport(...)`.
- **Given** a transport call is invoked with a context-cancellation mid-flight,
  **When** the cancellation fires during a retry backoff,
  **Then** the retry loop returns immediately rather than completing the
  remaining backoff sleep.
- **Given** a transport call receives a 500 response with a body containing
  a token,
  **When** the resulting error is logged or printed,
  **Then** the body is absent from the error string by default and only
  appears when the caller opts in via `httpx.WithBodyOnError(maxBytes)`.
- **Given** `go test -race ./internal/httpx/...` runs,
  **Then** all tests pass with race detection enabled and line coverage
  is ≥80%.

### US-V4-05: AKV writer consolidation (FR-V4-19..22)

**Description:** As a Go developer, I want one path to write to Azure Key
Vault, so that retry/redaction policy is uniform and the writer is finally
testable.

**Acceptance Criteria:**
- **Given** `internal/akvwriter` is the only AKV write path,
  **When** `cmd/saas-token-rotator` rotates a token,
  **Then** it calls `akvwriter.Put(ctx, name, value, Strategy)` and no
  open-coded AKV HTTP PUT remains in `saas-token-rotator/main.go`.
- **Given** `akvwriter.Put` is called for a soft-deleted secret with
  `Strategy = RecoverIfSoftDeleted`,
  **When** the request resolves,
  **Then** the secret is recovered and the new value written; with
  `Strategy = Overwrite`, the call returns
  `ErrSoftDeletedSecretExists` instead.
- **Given** the in-memory fake adapter in `akvwriter/fake_test.go`,
  **When** unit tests run,
  **Then** the fake satisfies the same `Put`/`Get` surface used by
  consumers, line coverage on `akvwriter` is ≥80%, and the typed errors
  (`ErrSecretNotFound`, `ErrAuthFailed`, `ErrConflict`,
  `ErrSoftDeletedSecretExists`) are each exercised by a test case.
- **Given** an AKV API call returns 5xx,
  **When** retry policy is exhausted,
  **Then** the returned error is typed and contains no raw response body
  (closes Q7 for this path).

### US-V4-06: Go binary lifecycle + per-binary runners (FR-V4-23..26)

**Description:** As a Go developer, I want lifecycle concerns (signals,
metrics, shutdown) in one place and per-binary runners in `internal/`, so
that orchestration is testable and `main.go` files are 30-line wiring
files.

**Acceptance Criteria:**
- **Given** `internal/bootstrap/SignalContext()` and `ServeMetrics(ctx, addr, registry)`
  exist,
  **When** `mgmt-leader-lease`, `controller-scaler`, `saas-token-rotator`,
  `argocd-jira-bridge` are inspected,
  **Then** each `main` uses `bootstrap.SignalContext()` instead of a local
  `signal.Notify(...)` setup and the metrics server (where present)
  comes from `bootstrap.ServeMetrics`.
- **Given** `internal/scaling/Runner` and `internal/bloblease/LeaseRunner`
  expose `Run(ctx) error`,
  **When** `cmd/controller-scaler/main.go` and `cmd/mgmt-leader-lease/main.go`
  are reviewed,
  **Then** each `main.go` is ≤40 lines and the run-loops live entirely
  inside the `internal/` packages.
- **Given** a SIGTERM is delivered during `saas-token-rotator`'s rotation
  grace period,
  **When** shutdown fires,
  **Then** the process exits within 1 second (no blocking `time.Sleep`
  remains) and the metrics server shuts down cleanly.
- **Given** unit tests run for `scaling.Runner`, `bloblease.LeaseRunner`,
  and `rotation.Runner`,
  **Then** the runners are exercised with a fake Kubernetes client, a
  fake blob client, and a fake AKV/SaaS client respectively and combined
  line coverage across the three runner packages is ≥80%.

### US-V4-07: service_seed three-module split (FR-V4-27..31)

**Description:** As a platform engineer maintaining the service-seed
tooling, I want the 981-line `seed_job.py` split into three modules with
clear contracts, so that each concern (Jira parse, template render, gitops
PR) is independently testable.

**Acceptance Criteria:**
- **Given** `jira_intake.py`, `service_template.py`, `gitops_pr.py`, and
  `cli.py` exist,
  **When** `seed_job.py` is reviewed,
  **Then** the file is either deleted or reduced to a deprecation shim
  re-exporting the new module entry points; no concern (parse / render /
  git) lives across multiple modules.
- **Given** `ServiceRequest` is a frozen `@dataclass` with
  `__post_init__` validation,
  **When** the CLI receives a Jira issue with a missing required field,
  **Then** `jira_intake.parse(...)` raises a typed exception with the
  field name and `cli.py` exits non-zero with a clear message.
- **Given** `tools/service_seed/tests/` runs,
  **When** coverage is computed,
  **Then** combined line coverage across `jira_intake.py`,
  `service_template.py`, `gitops_pr.py` is ≥75% and each module has a
  dedicated test file with mocked subprocess/HTTP/cookiecutter.
- **Given** `pyproject.toml` exists at `tools/service_seed/`,
  **When** `pip install -e tools/service_seed` runs,
  **Then** the package installs with pinned dependencies and the CLI is
  exposed as a console_script entry point.

### US-V4-08: Jinja templates + SLO/rollout profiles (FR-V4-32..35)

**Description:** As a platform engineer, I want YAML output driven by
templates + profile data, so that adding an SLO class or modifying a CRD
is a YAML edit rather than a Python edit, and rendered output is schema-
validated.

**Acceptance Criteria:**
- **Given** `tools/service_seed/templates/` contains `.yaml.j2` files
  mirroring rendered output structure,
  **When** `service_template.render(req, registry)` is invoked,
  **Then** every output file is template-driven and zero YAML literals
  remain inside `service_template.py`.
- **Given** Jinja2 runs with `StrictUndefined`,
  **When** a template references a variable absent from the render context,
  **Then** rendering fails loudly with the missing variable name (caught
  in unit tests).
- **Given** `profiles/slo.yaml` and `profiles/rollout.yaml` exist with
  `gold`/`silver`/`bronze` keys,
  **When** `service_template.render(req=..., slo_class="gold", ...)` runs,
  **Then** thresholds and step strategies come from the profile YAML and
  zero SLO numerics (`0.99`, `500`, `5m`) appear as Python literals in
  `service_template.py`.
- **Given** the CI `kubeconform` step runs against rendered output for
  each SLO class fixture,
  **When** any rendered manifest fails schema validation,
  **Then** the CI job fails the PR.

### US-V4-09: Hybrid Helm → ESO secret migration (QW-SEC: FR-V4-36..40)

**Description:** As a security-conscious platform engineer, I want all
sensitive material to flow through ESO + AKV with zero `helm_release.set
{ value = sensitive }` shortcuts, so that no secret material appears in
Terraform plan output, state file plaintext, or CI logs.

**Acceptance Criteria:**
- **Given** the `secrets_managed_in_tf` and `secrets_referenced_only`
  helper locals exist in `terraform/locals.tf`,
  **When** any new `helm_release` is added or modified,
  **Then** every secret-bearing value reference goes through one of the
  two modes and the rendered chart consumes the secret via
  `ExternalSecret`.
- **Given** the custom tflint rule for FR-V4-37 runs in CI,
  **When** a PR introduces `helm_release.set { name = X, value =
  var.<sensitive_var> }`,
  **Then** the CI job fails the PR with a pointer to FR-V4-37.
- **Given** the Appendix A inventory of legacy `helm_release.set { value
  = sensitive_var }` call sites,
  **When** P5 sprints close out the migration,
  **Then** the count of in-tree call sites reaches zero (success-metric
  verification) and each migrated site has a paired PR linking
  TF → AKV → ESO → chart.
- **Given** FR-V4-18 (httpx redaction-by-default) is in effect,
  **When** an AKV/Bitbucket/Jira call fails with an HTTP 4xx/5xx,
  **Then** the resulting error contains no token material (closes Q7).

### US-V4-10: Robustness pass (QW-ROB: FR-V4-41..44)

**Description:** As a platform engineer, I want stateful storage,
cross-variable invariants, network timeouts, and template-path validation
in place, so that accidental destruction, configuration corruption, hanging
processes, and path-traversal exploits are foreclosed.

**Acceptance Criteria:**
- **Given** `terraform/storage.tf` (state lease container) and
  `terraform/velero.tf` (Velero backup container) declare `lifecycle {
  prevent_destroy = true }`,
  **When** a PR proposes destroying either resource,
  **Then** `terraform plan` errors with the prevent-destroy message and
  the destruction requires a deliberate two-PR sequence.
- **Given** `precondition{}` blocks are added per FR-V4-42,
  **When** `terraform plan` runs with a spoke CIDR overlapping the hub
  CIDR or a `region_abbrev` inconsistent with `region`,
  **Then** the plan fails with the named invariant before any resource is
  evaluated.
- **Given** `jira_intake.py`, `gitops_pr.py`, and any subprocess/urllib
  call in `service_template.py` declare explicit timeouts (HTTP 30s,
  subprocess 300s),
  **When** a network or git operation hangs,
  **Then** the call returns with a timeout error within the declared bound
  and no credential material appears in the resulting log line.
- **Given** the cookiecutter fallback renderer is invoked with a template
  whose filename contains `../`,
  **When** rendering attempts to write outside the destination,
  **Then** the renderer raises with a path-traversal error before writing
  any file (closes Q9).

### US-V4-11: CI hygiene + version source-of-truth (QW-CI: FR-V4-45..49)

**Description:** As a platform engineer, I want one source of truth for
tool versions, every GitHub Action pinned to a SHA, baseline suppressions
with rationale + expiry, and pre-commit parity with CI, so that drift,
supply-chain risk, and stale suppressions don't accrete.

**Acceptance Criteria:**
- **Given** `.tool-versions` declares versions for terraform, tflint,
  checkov, terragrunt, kubectl, kustomize,
  **When** any CI workflow installs a tool,
  **Then** the version is read from `.tool-versions` via the asdf/mise
  action and no other file in the repo declares those tool versions.
- **Given** every `uses:` in `.github/workflows/*.yml` is pinned to a
  40-character SHA with a human-readable tag in a comment,
  **When** Dependabot opens a weekly upgrade PR,
  **Then** the PR updates SHAs together with comment tags and is
  auto-mergeable subject to standard PR review.
- **Given** `.checkov.baseline` carries per-finding `rationale`,
  `owner`, `expiry` metadata,
  **When** a finding's `expiry` lapses or any field is missing,
  **Then** the CI Checkov job fails the PR with a pointer to the offending
  entry.
- **Given** `pre-commit run --all-files` is invoked in CI after a clean
  run,
  **Then** the resulting diff is empty (no drift between pre-commit and CI
  Checkov/tflint scopes; FR-V4-48 and FR-V4-49 satisfied).

---

## Non-Goals (Out of Scope)

### Bucket 1 — ADR-locked items re-stated (because v4 touches their surface)

- **No HashiCorp Vault or alternative secrets store.** ADR-005-v2 stays:
  ESO + AKV is the secrets transport.
- **No replacement of Terraform with Pulumi/Bicep.** Anti-rabbit-hole; not
  reopened.
- **No Jenkins → GitHub Actions for application CI.** ADR-001-v2 locks
  Jenkins; v4 touches only platform-repo CI.
- **No CRD-based cluster registry on mgmt-we.** Rejected during grilling;
  committed YAML chosen (codified in ADR-031-v4).
- **No customer-managed-keys (CMK) for the state SA.** v3 deferred to
  roadmap; v4 holds with Microsoft-managed keys.

### Bucket 2 — Architecture options explicitly rejected during grilling

- **No Pydantic typed models for CRD schemas.** Jinja2 + YAML profiles
  chosen (FR-V4-32..35).
- **No extension of cookiecutter to gitops manifests.** Cookiecutter
  remains scoped to the service repo template; gitops manifests use a
  separate Jinja template tree.
- **No fully out-of-band Helm secrets** (i.e., operator-only AKV
  population). Hybrid by rotation ownership chosen (FR-V4-36).
- **No single reusable workflow with `mode=plan|apply` input.** Two
  workflows + composite action chosen (FR-V4-10..12).
- **No shared `Runner` interface across Go binaries.** Per-binary runners
  chosen (FR-V4-24).
- **No map-of-identities single TF module.** Single-identity, caller-owned
  `for_each` chosen (FR-V4-05).
- **No `secrets/` shared package in Go.** Consumer-defined interfaces
  chosen until a second non-Azure backend exists.

### Bucket 3 — Feature / area exclusions

- **No multi-cloud cluster registry.** Azure-only; multi-cloud is a future
  PRD.
- **No auto-generation of the cluster registry** from CSP discovery. The
  registry remains human-curated YAML.
- **No Backstage UI changes.** Sonar instrumentation in v3 is sufficient
  for v4.
- **No Argo Rollouts strategy library expansion** beyond the
  gold/silver/bronze SLO profiles.
- **No application-level changes** to `gitops/apps/myapp` beyond what
  template rendering (FR-V4-32) produces.
- **No modification of v2/v3 FRs, ADRs, or user stories.** v4 is additive
  only.

---

## Technical Considerations

### Phasing

| Phase | Stories | Rationale |
|---|---|---|
| **P0 — Foundation** | US-V4-11 (`.tool-versions`, SHA-pin, baseline schema, pre-commit/CI parity) | No deps; cleans drift surface before any other PR is opened |
| **P1 — Registry + workflows** | US-V4-01 (cluster registry + ADR-031-v4), US-V4-03 (reusable workflows) | Independent of code refactors; high-leverage foundations |
| **P2 — Self-contained TF + Robustness** | US-V4-02 (workload-identity module), US-V4-10 (robustness pass) | No code-graph deps on P3+; can run in parallel with P1 with capacity |
| **P3 — Go deepening** | US-V4-04 (httpx) → US-V4-05 (akvwriter); US-V4-06 (bootstrap + runners) in parallel | US-V4-04 strictly precedes US-V4-05 |
| **P4 — Python deepening** | US-V4-07 (three-module split) → US-V4-08 (Jinja templates + profiles) | US-V4-07 strictly precedes US-V4-08; depends on US-V4-01 from P1 |
| **P5 — Secrets migration** | US-V4-09 (Helm → ESO, per-secret) | Longest tail; benefits from US-V4-04 transport + US-V4-05 writer |

### Testing Strategy

The PRD adopts a uniform testing floor across all v4 stories:

| Surface | Floor |
|---|---|
| Go `internal/*` packages touched by US-V4-04..06 | ≥80% line coverage on changed packages; `go test -race ./...` enabled; table-driven tests for retry, redaction, runner state machines |
| Python `service_seed/*` after US-V4-07/08 | ≥75% line coverage; `pytest --cov` enforced in CI; `mypy --strict` on the new modules |
| Terraform `modules/workload_identity` (US-V4-02) | `terraform test` mandatory; `precondition` invariants exercised; `terraform validate` + `tflint` clean |
| GH reusable workflows (US-V4-03) | `act`-based smoke test in CI confirming `workflow_call` inputs accepted; plan-truncation logic unit-tested |
| Rendered Jinja templates (US-V4-08) | `kubeconform` against rendered output for each SLO class; CI fails on schema mismatch |
| Cluster registry (US-V4-01) | JSON-schema validation in pre-commit + CI; round-trip test that `yamldecode` of registry into TF locals matches expected shape |

Coverage deltas apply to *touched* packages only; legacy untouched code is
grandfathered. Test fixtures share a single `tools/service_seed/tests/fixtures/registry.yaml`
to close the topology-encoded-thrice loop in tests too.

### Risks

| ID | Risk | Mitigation |
|---|---|---|
| R-V4-1 | **Cluster registry adoption gaps** — TF, ArgoCD locals, and `seed_job` migration land out of order, causing partial drift during P1 | Single P1 release-train; all three consumers convert in the same merge window; registry schema validation blocks early divergence |
| R-V4-2 | **`workload_identity` module breaks identities mid-migration** — federated-credential subject formatting differs from current resources, causing transient auth failures | Migrate one identity per PR; `terraform plan` diff inspected to confirm only identity-resource adds, no destroys; rollback path is a single `terraform apply` of the previous module-free TF |
| R-V4-3 | **Reusable workflows break apply ordering** — two-phase matrix dependency mis-wired, allowing workload apply before mgmt apply | `act` smoke test asserts `needs:` declarations before merge; canary deploy: ship the workflow with `if: github.ref != 'refs/heads/main'` first, observe a dry run, then enable |
| R-V4-4 | **`httpx` redaction over-redacts** — masks information needed for triage | `WithBodyOnError(maxBytes)` opt-in escape hatch; redactor patterns are tested with positive + negative fixtures; on-call playbook updated to set the opt-in for diagnostic windows |
| R-V4-5 | **service_seed split breaks active onboarding flows** — a Jira-driven service request mid-flight fails because intermediate state moved between modules | Feature-flag the split (`SEED_USE_LEGACY=true`) for one sprint; run both code paths in dry-run mode; cut over only after parity confirmed |
| R-V4-6 | **Helm → ESO migration regression** — chart that previously consumed Helm-set secret values silently uses an empty value when migrated to `ExternalSecret` | Pre-migration validation: render the chart with both old and new value paths, diff the rendered output; ESO `refreshInterval` checked before cutover; per-secret rollback runbook |
| R-V4-7 | **`.tool-versions` adoption breaks contributors without asdf/mise** | Provide a bootstrap script (`scripts/install-tools.sh`) that reads `.tool-versions` and installs via `tfenv`/direct download; documented in onboarding |
| R-V4-8 | **ADR-031-v4 not approved before code lands** | FR-V4-01..04 demote to `OQ-V4-NN`; the rest of v4 can ship; cluster registry work parked until ADR-031-v4 lands |

---

## Success Metrics

- **Cluster-topology drift incidents** (registry vs actual): 0 in any
  rolling quarter post-US-V4-01.
- **Go `internal/*` line coverage** on changed packages: ≥80%.
- **Python `service_seed` line coverage** post-split: ≥75%.
- **v4-touched TF resources** with `precondition{}` or `terraform test`:
  100%.
- **`helm_release.set { value = sensitive_var }`** for system-generated
  secrets: 0 by P5 close.
- **GitHub Actions pinned to commit SHAs:** 100%.
- **Files declaring TF/tflint/checkov versions:** exactly 1
  (`.tool-versions`).
- **Identity-setup PR diff size** for a new workload identity: ≥60%
  smaller than pre-v4 baseline.
- **Plan-output truncation implementations** across CI + apply: exactly 1.
- **`kubeconform` failures** on rendered Jinja templates in CI: 0.
- **`seed_job.py`** as a single 981-line file: deleted (replaced by 3
  modules + thin CLI).
- **Lines per `main.go`** in `tools/mgmt-plane-lock/cmd/*/`: ≤40.

---

## Open Questions

- **OQ-V4-01** — Per-environment registry overlays. Does
  `gitops/clusters/registry.yaml` need per-env override files (e.g.,
  `registry.dev.yaml` patching the base), or is one file with env-keyed
  maps sufficient? Defer until a second environment diverges from the
  base shape.
- **OQ-V4-02** — SLO profile drift detection. Should
  `tools/service_seed/profiles/slo.yaml` be enforceable via a Kyverno
  policy that rejects `Rollout` manifests not matching a known profile?
  Cross-domain with ADR-021; defer to a future PRD.
- **OQ-V4-03** — Helm → ESO migration ownership and prioritization.
  Platform team vs operator team: who owns the per-secret migration
  calendar? Resolve before P5 begins.
- **OQ-V4-04** — Fate of `tools/mgmt-plane-lock/cmd/mgmt-cli`. The
  architecture review surfaced no clear purpose post-US-V4-06. Decide
  before P3 begins whether to merge into `mgmt-leader-lease` or retain
  as an operator escape hatch.
- **OQ-V4-05** — Test-fixture cluster registry granularity. Single
  shared fixture or per-test fixture? Defer to implementation.
- **OQ-V4-06** — Silver SLO success-rate threshold. `docs/architect.md`
  §10.2 lists Silver as success-rate ≥ 99% — identical to Gold (which
  adds a latency gate). Confirm whether Silver is intentionally
  Gold-without-latency or whether Silver should have a lower
  success-rate threshold (e.g., 99.5%). Resolve before US-V4-08 (Jinja
  templates + SLO profiles) ships.
- ~~**OQ-V4-07**~~ — **Resolved 2026-05-26.** TLS history purge
  remediation strategy. User chose full remediation via destructive
  `git filter-repo` rewrite + force-push + cert re-revocation +
  re-clone runbook. Implementation captured in PRD-v3.1 as
  `FR-V3.1-06..09` and `US-V3.1-02`. v4 P0 cannot begin until PRD-v3.1
  US-V3.1-02 ships. See §Pre-existing v3 obligations for status.

---

## Pre-existing v3 Obligations

PRD-v4 P0 entry is gated on a v3 readiness verification (executed
2026-05-26). The audit found that 22 of 28 v3 FRs were present, 1 was
absent, and 5 were partial. This section records the disposition of
each non-clean finding so v4 work proceeds from an explicit baseline.

### Accepted exceptions (carve-outs at v4 entry)

| Item | Finding | Disposition | Tracking |
|---|---|---|---|
| **FR-V3-10** — Crossplane UAMI retains subscription-Contributor scope (`terraform/main.tf:249-250`) | Crossplane's cross-RG composition pattern cannot be expressed via per-RG identity without substantial composition redesign | Accepted as time-bounded exception; reviewed at next platform-architecture review | `ADR-024-v3` Amendment 2026-05-26 — Crossplane UAMI Carve-Out |

### v3.1 patch obligations (must close before v4 P0 begins)

| Item | Finding | Disposition | Tracking |
|---|---|---|---|
| **FR-V3-12** — AKV-native quarterly rotation policies absent from `terraform/keyvaults.tf` (zero `rotation_policy` blocks) | Patch in PRD-v3.1 (`FR-V3.1-01..05`); blocks v4 P0 start until merged | `_docs/IDP-GitOps-Blueprint-PRD-v3.1.md` US-V3.1-01 |
| **FR-V3-05** — TLS history purge incomplete: `git filter-repo` rewrite was never performed; `BEGIN PRIVATE KEY` blobs remain reachable from `7b07f95` (original commit) and `9d04b71` (the `git rm` commit, which is not a history rewrite) on the canonical remote | **Resolution chosen 2026-05-26: full remediation** via destructive history rewrite + force-push + cert re-revocation + re-clone runbook. Captured as `FR-V3.1-06..09` and `US-V3.1-02` in PRD-v3.1. OQ-V4-07 is closed by this decision | `_docs/IDP-GitOps-Blueprint-PRD-v3.1.md` US-V3.1-02 |

### Minor partials (accepted as alternative implementations)

| Item | Finding | Disposition |
|---|---|---|
| **FR-V3-20** — Custom "unvalidated-variable detector" was delivered as `scripts/check-var-validation.py` (Python companion script wired into CI advisory at `.github/workflows/terraform-ci.yml:121-124`) rather than as a native tflint custom plugin | Accepted as functionally equivalent; the FR text mandated "custom tflint rule" but the goal (unvalidated-variable detection in CI) is achieved by the Python companion. No corrective work required |
| **FR-V3-23** — Three `random_uuid` resources cover Backstage Entra app-role IDs (`terraform/main.tf:291-303`); no remaining `uuid()` interpolations anywhere in `terraform/*.tf` | Effectively complete. The "partial" tag in the audit reflected the narrower coverage scope (only Backstage Entra), not a remaining `uuid()` call — there are none |

### Readiness gate

- **PRD-v3.1 must merge before v4 P0 starts.** PRD-v3.1 carries two
  user stories:
  - `US-V3.1-01` (AKV-native rotation policies) closes the
    prerequisite for `US-V4-09` (Helm → ESO migration).
  - `US-V3.1-02` (TLS history purge via `git filter-repo`) closes
    the residual FR-V3-05 gap and resolves OQ-V4-07.
- **Crossplane carve-out has no gate impact.** It is documented at v4
  entry and reviewed quarterly thereafter (per ADR-024-v3 Amendment
  2026-05-26).
- **No other v3 obligation blocks v4 P0** — the audit's minor partials
  (FR-V3-20, FR-V3-23) are accepted as functionally equivalent.

---

## Appendix A — Helm Set-Value Inventory (drives FR-V4-39)

The following `helm_release.set { value = <sensitive> }` call sites are
known and assigned migration modes by P5:

| Site | Variable | Mode | Notes |
|---|---|---|---|
| `terraform/main.tf` (PostgreSQL admin password injected into chart) | `postgres_admin_password` | `secrets_managed_in_tf` | TF-generated via `random_password` (after switch from current `var`) |
| `terraform/main.tf` (Backstage TLS key/cert) | `backstage_tls_key`, `backstage_tls_crt` | `secrets_referenced_only` | Issued by AKV (per v3 FR-V3-06), referenced by data source |
| `terraform/jenkins.tf` (Jenkins admin password) | `jenkins_admin_password` | `secrets_managed_in_tf` | Internal-only; TF-generated |
| `terraform/jenkins.tf` (Bitbucket workspace token) | `jenkins_bitbucket_workspace_token` | `secrets_referenced_only` | Operator-rotated; pre-populated in AKV |
| Additional sites discovered during P5 audit | _TBD_ | _TBD_ | Inventory amended via PR to this appendix |

---

## Pending Approvals (referenced from §Purpose)

- **ADR-031-v4 — Cluster topology lives in a single committed registry.**
  Status: **Accepted (PRD-v4 approved 2026-05-26)**. Co-approved with
  this PRD; FR-V4-01..04 and US-V4-01 are in v4 scope. This entry is
  retained as a historical record of the co-approval gate.

No other v2 / v3 ADR is modified by this PRD. Any future need to amend
an existing ADR is itself a Pending Approval.