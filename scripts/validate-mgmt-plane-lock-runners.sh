#!/usr/bin/env bash
# validate-mgmt-plane-lock-runners.sh — pre-commit + CI guard for US-V4-06
# (FR-V4-23..26): per-binary runner refactor of the mgmt-plane-lock cmd
# suite.
#
# Asserts:
#   - cmd/{controller-scaler,mgmt-leader-lease,saas-token-rotator,argocd-jira-bridge}/main.go
#     are each ≤40 lines (FR-V4-24).
#   - No `signal.Notify` or `os/signal` import remains in any cmd/* file
#     (FR-V4-23: signal handling lives in internal/bootstrap.SignalContext).
#   - `go build`, `go vet`, and `go test ./...` are clean for the module.
#
# Skips the build/test steps when go is unavailable (e.g., minimal CI runners
# that only invoke the static checks) — those run in the dedicated
# validate-mgmt-plane-lock-runners CI job.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
mod_dir="$repo_root/tools/mgmt-plane-lock"

if [ ! -d "$mod_dir" ]; then
    echo "::error::expected $mod_dir to exist"
    exit 1
fi

fail=0
for f in cmd/controller-scaler/main.go cmd/mgmt-leader-lease/main.go \
         cmd/saas-token-rotator/main.go cmd/argocd-jira-bridge/main.go; do
    path="$mod_dir/$f"
    if [ ! -f "$path" ]; then
        echo "::error file=tools/mgmt-plane-lock/$f::file missing"
        fail=1
        continue
    fi
    lines=$(wc -l < "$path")
    if [ "$lines" -gt 40 ]; then
        echo "::error file=tools/mgmt-plane-lock/$f::main.go is $lines lines, FR-V4-24 limit is 40"
        fail=1
    fi
done

# Forbid direct os/signal use in cmd/*.go — must go through
# bootstrap.SignalContext (FR-V4-23).
if grep -RnE '"os/signal"|signal\.Notify' "$mod_dir/cmd/" >/dev/null 2>&1; then
    grep -RnE '"os/signal"|signal\.Notify' "$mod_dir/cmd/" || true
    echo "::error::cmd/* mains MUST use bootstrap.SignalContext (FR-V4-23) — direct os/signal use is forbidden"
    fail=1
fi

if command -v go >/dev/null 2>&1; then
    (
        cd "$mod_dir"
        go build ./...
        go vet ./...
        go test ./...
    )
fi

exit "$fail"
