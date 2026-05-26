#!/usr/bin/env bash
# validate-rendered-manifests.sh — US-V4-08 (FR-V4-32..35) AC4.
#
# Renders the service-seed GitOps manifest set for each SLO class fixture
# (gold/silver/bronze) into a fresh tempdir and runs ``kubeconform`` against
# the rendered output.  Built-in k8s schemas validate the core resources
# (Namespace, Service, ServiceAccount, Deployment, Ingress, ConfigMap);
# ``--ignore-missing-schemas`` allows the platform CRDs (SQLDatabase,
# CosmosAccount, ServiceBus, NamespaceRolloutPolicy, Rollout,
# AnalysisTemplate, ExternalSecret, Kustomization) to pass through without
# a network round-trip — the templates themselves are the schema for those.
#
# Exits non-zero on the first kubeconform failure so the CI job blocks merge.
# When ``kubeconform`` is not on $PATH the script exits 0 with a warning so
# local pre-commit users without the binary aren't blocked; CI installs the
# binary explicitly so the gate is enforced before merge.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v kubeconform >/dev/null 2>&1; then
    echo "warn: kubeconform not found on PATH — skipping rendered-manifest validation"
    echo "      install via 'brew install kubeconform' or download from"
    echo "      https://github.com/yannh/kubeconform/releases"
    exit 0
fi

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# Restrict kubeconform to *.yaml files that look like a Kubernetes manifest.
# Kustomization files (kind: Kustomization) and the bronze analysis-template
# comment stub (no apiVersion) are skipped — they're not k8s objects.
filter_manifests() {
    local dir="$1"
    find "$dir" -name '*.yaml' -type f -print0 | while IFS= read -r -d '' f; do
        if head -1 "$f" | grep -q '^apiVersion:'; then
            if head -2 "$f" | grep -q '^kind: Kustomization$'; then
                continue
            fi
            printf '%s\0' "$f"
        fi
    done
}

fail=0
for slo in gold silver bronze; do
    fixture_dir="$WORKDIR/$slo"
    mkdir -p "$fixture_dir"
    echo "==> rendering $slo fixture into $fixture_dir"
    python3 -m tools.service_seed.cli generate-gitops \
        --service-name "fixture-$slo" \
        --slo-class "$slo" \
        --output-dir "$fixture_dir" >/dev/null

    manifests=$(filter_manifests "$fixture_dir" | xargs -0 -r echo)
    if [ -z "$manifests" ]; then
        echo "::error::no rendered manifests produced for $slo"
        fail=1
        continue
    fi

    echo "==> running kubeconform on $slo fixture"
    # shellcheck disable=SC2086
    if ! kubeconform \
        --strict \
        --ignore-missing-schemas \
        --kubernetes-version 1.31.0 \
        --summary \
        $manifests; then
        echo "::error::kubeconform failed for slo_class=$slo"
        fail=1
    fi
done

if [ "$fail" -ne 0 ]; then
    exit 1
fi
echo "All SLO class fixtures pass kubeconform."
