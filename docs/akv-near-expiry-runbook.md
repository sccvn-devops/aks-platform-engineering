# AKV Near-Expiry Runbook

**Owner**: Platform on-call
**Trigger**: `Microsoft.KeyVault.SecretNearExpiry`, `Microsoft.KeyVault.CertificateNearExpiry`, or `Microsoft.KeyVault.KeyNearExpiry` Event Grid event delivered to the platform on-call inbound webhook 14–30 days before expiry.

## Alert payload

The Event Grid delivery includes:

- `data.VaultName` — Azure Key Vault hosting the expiring object
- `data.ObjectName` — secret / certificate / key name
- `data.ObjectType` — `Secret`, `Certificate`, or `Key`
- `data.ExpiryDateUtc` — RFC3339 expiry timestamp
- HTTP headers added by Terraform-declared delivery properties:
  - `X-Platform-Runbook` — link back to this document
  - `X-Platform-Cluster` — logical cluster identifier (e.g. `mgmt-we`, `aks-prod-we`)

## Triage

1. **Identify the resource** from `VaultName + ObjectName`. Cross-check the catalogue at the top of `terraform/keyvaults.tf` to confirm mode (`operator-supplied`, `platform-generated`, or `akv-issued`) and the owning UAMI.
2. **Confirm whether AKV rotation is healthy**:
   - `akv-issued` certificates auto-renew at `lifetime_percentage = 75`. Validate via `az keyvault certificate show --vault-name <vault> --name <object>` and check `attributes.expires` is in the future.
   - `platform-generated` secrets (Bitbucket / Jira tokens) are rotated by `saas-token-rotator`. Confirm the latest `CronJob` run completed successfully and writes to both regional AKVs.
   - `operator-supplied` secrets (Jenkins admin password, Backstage TLS PEMs) require a manual rotation PR.
3. **Rotate**:
   - Operator-supplied: open a PR updating the relevant `var.<name>` via `scripts/fetch-tf-secrets.sh`, then re-apply Terraform; the new secret version will inherit the next quarterly `expiration_date`.
   - Platform-generated: trigger an immediate run of the `saas-token-rotator` CronJob (`kubectl create job --from=cronjob/saas-token-rotator ...`); verify the rotated secret carries a new `expiration_date`.
   - AKV-issued: if AutoRenew has not fired, force a renewal with `az keyvault certificate get-default-policy ...` followed by `az keyvault certificate create`.
4. **Verify ESO re-sync**: `kubectl get externalsecret -A | grep <secret-name>` to confirm `SyncedToTarget` becomes `True` within 60 s.

## Escalation

- If the rotation UAMI lacks the required role assignment on the target vault, page the platform engineer who owns `terraform/saas_token_rotator.tf`.
- If AutoRenew is not firing on an AKV-issued certificate, open a Microsoft support case (Severity B) — this indicates a regression in the Key Vault control plane and is outside the platform's repair scope.

## Closing the alert

After rotation completes:

- Confirm `az keyvault {secret|certificate} show` reports the new `attributes.expires`.
- Comment on the inbound-webhook delivery in the on-call channel with the rotation evidence.
- The next AKV-emitted event will not fire for at least 30 days, satisfying the alert dedup window.
