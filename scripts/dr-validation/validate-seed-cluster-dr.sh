#!/usr/bin/env bash
# Validates catastrophic management-plane recovery from seed-wus as per US-025.
#
# Prerequisites:
#   - kubectl authenticated to seed-wus and workload clusters
#   - seed-wus already has Crossplane installed
#   - bootstrap/control-plane-claim.yaml placeholders have been replaced
#
# Usage:
#   ./scripts/dr-validation/validate-seed-cluster-dr.sh
#   ./scripts/dr-validation/validate-seed-cluster-dr.sh --apply-claim
#   WORKLOAD_CONTEXTS="aks-dev-we-admin aks-staging-we-admin aks-prod-we-admin aks-prod-ne-admin" \
#     ./scripts/dr-validation/validate-seed-cluster-dr.sh --apply-claim

set -euo pipefail

SEED_CONTEXT="${SEED_CONTEXT:-seed-wus-admin}"
CROSSPLANE_NAMESPACE="${CROSSPLANE_NAMESPACE:-crossplane-system}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-argocd}"
STATUS_NAMESPACE="${STATUS_NAMESPACE:-kube-system}"
STATUS_CONFIGMAP="${STATUS_CONFIGMAP:-mgmt-leader-status}"
CLAIM_FILE="${CLAIM_FILE:-bootstrap/control-plane-claim.yaml}"
WORKLOAD_CONTEXTS="${WORKLOAD_CONTEXTS:-}"
WORKLOAD_PROBES="${WORKLOAD_PROBES:-}"
WORKLOAD_PROBE_INTERVAL_SECONDS="${WORKLOAD_PROBE_INTERVAL_SECONDS:-15}"

MAX_RECOVERY_SECONDS="${MAX_RECOVERY_SECONDS:-2700}"
CLAIM_READY_TIMEOUT_SECONDS="${CLAIM_READY_TIMEOUT_SECONDS:-2400}"
CLUSTER_BOOTSTRAP_TIMEOUT_SECONDS="${CLUSTER_BOOTSTRAP_TIMEOUT_SECONDS:-900}"
LEASE_READY_TIMEOUT_SECONDS="${LEASE_READY_TIMEOUT_SECONDS:-600}"
APPLICATION_HEALTH_TIMEOUT_SECONDS="${APPLICATION_HEALTH_TIMEOUT_SECONDS:-600}"

APPLY_CLAIM=false
KEEP_TEMP_KUBECONFIG=false
TEMP_KUBECONFIG=""
RECOVERY_START=0
WORKLOAD_MONITOR_PID=""
WORKLOAD_MONITOR_STATUS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply-claim)
      APPLY_CLAIM=true
      ;;
    --keep-temp-kubeconfig)
      KEEP_TEMP_KUBECONFIG=true
      ;;
    *)
      echo "usage: $0 [--apply-claim] [--keep-temp-kubeconfig]" >&2
      exit 1
      ;;
  esac
  shift
done

log() {
  echo "[$(date -u '+%H:%M:%S')] $*" >&2
}

cleanup() {
  if [[ -n "$WORKLOAD_MONITOR_PID" ]]; then
    kill "$WORKLOAD_MONITOR_PID" >/dev/null 2>&1 || true
    wait "$WORKLOAD_MONITOR_PID" >/dev/null 2>&1 || true
  fi
  if [[ "$KEEP_TEMP_KUBECONFIG" == false && -n "$TEMP_KUBECONFIG" && -f "$TEMP_KUBECONFIG" ]]; then
    rm -f "$TEMP_KUBECONFIG"
  fi
  if [[ -n "$WORKLOAD_MONITOR_STATUS" && -f "$WORKLOAD_MONITOR_STATUS" ]]; then
    rm -f "$WORKLOAD_MONITOR_STATUS"
  fi
}

trap cleanup EXIT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "FAIL: required command not found in PATH: $1" >&2
    exit 1
  fi
}

claim_field() {
  local jsonpath=$1
  kubectl create --dry-run=client -f "$CLAIM_FILE" -o "jsonpath=${jsonpath}"
}

jsonpath_or_empty() {
  local kubeconfig_args=("$@")
  "${kubeconfig_args[@]}" 2>/dev/null || true
}

validate_claim_placeholders() {
  local placeholders
  placeholders=$(grep -o 'REPLACE_WITH_[A-Z0-9_]*' "$CLAIM_FILE" | sort -u || true)
  if [[ -n "$placeholders" ]]; then
    echo "FAIL: ${CLAIM_FILE} still contains placeholder values:" >&2
    echo "$placeholders" >&2
    exit 1
  fi
}

wait_for_claim_ready() {
  local claim_name=$1 claim_namespace=$2 timeout_seconds=$3
  kubectl --context="$SEED_CONTEXT" wait \
    --for=condition=Ready=True \
    "aksclusterclaim/${claim_name}" \
    -n "$claim_namespace" \
    --timeout="${timeout_seconds}s"
}

wait_for_connection_secret() {
  local claim_namespace=$1 secret_name=$2 timeout_seconds=$3
  local data

  for _ in $(seq 1 "$timeout_seconds"); do
    data=$(kubectl --context="$SEED_CONTEXT" get secret "$secret_name" -n "$claim_namespace" -o jsonpath='{.data.kubeconfig}' 2>/dev/null || true)
    if [[ -n "$data" ]]; then
      TEMP_KUBECONFIG=$(mktemp)
      printf '%s' "$data" | base64 --decode >"$TEMP_KUBECONFIG"
      log "Recovered kubeconfig written to $TEMP_KUBECONFIG"
      return 0
    fi
    sleep 1
  done

  echo "FAIL: connection secret ${claim_namespace}/${secret_name} did not expose kubeconfig within ${timeout_seconds}s" >&2
  exit 1
}

wait_for_bootstrap_application() {
  local timeout_seconds=$1 expected_repo=$2 expected_path=$3
  local repo path

  for _ in $(seq 1 "$timeout_seconds"); do
    repo=$(kubectl --kubeconfig="$TEMP_KUBECONFIG" get application cluster-bootstrap -n "$ARGOCD_NAMESPACE" -o jsonpath='{.spec.source.repoURL}' 2>/dev/null || true)
    path=$(kubectl --kubeconfig="$TEMP_KUBECONFIG" get application cluster-bootstrap -n "$ARGOCD_NAMESPACE" -o jsonpath='{.spec.source.path}' 2>/dev/null || true)
    if [[ -n "$repo" && -n "$path" ]]; then
      if [[ "$repo" != "$expected_repo" ]]; then
        echo "FAIL: cluster-bootstrap repoURL is ${repo}, expected ${expected_repo}" >&2
        exit 1
      fi
      if [[ "$path" != "$expected_path" ]]; then
        echo "FAIL: cluster-bootstrap path is ${path}, expected ${expected_path}" >&2
        exit 1
      fi
      log "OK: cluster-bootstrap points to ${repo}:${path}"
      return 0
    fi
    sleep 2
  done

  echo "FAIL: cluster-bootstrap Application was not created within ${timeout_seconds}s" >&2
  exit 1
}

wait_for_leadership_active() {
  local timeout_seconds=$1
  local status elapsed lease_blob_url

  for _ in $(seq 1 "$timeout_seconds"); do
    status=$(kubectl --kubeconfig="$TEMP_KUBECONFIG" get configmap "$STATUS_CONFIGMAP" -n "$STATUS_NAMESPACE" -o jsonpath='{.data.leadershipStatus}' 2>/dev/null || true)
    lease_blob_url=$(kubectl --kubeconfig="$TEMP_KUBECONFIG" get configmap "$STATUS_CONFIGMAP" -n "$STATUS_NAMESPACE" -o jsonpath='{.data.leaseBlobURL}' 2>/dev/null || true)
    if [[ "$status" == "active" && -n "$lease_blob_url" ]]; then
      elapsed=$(($(date +%s) - RECOVERY_START))
      log "OK: recovered management cluster acquired the blob lease in ${elapsed}s"
      return 0
    fi
    sleep 2
  done

  echo "FAIL: recovered management cluster did not report active leadership within ${timeout_seconds}s" >&2
  kubectl --kubeconfig="$TEMP_KUBECONFIG" get configmap "$STATUS_CONFIGMAP" -n "$STATUS_NAMESPACE" -o yaml || true
  exit 1
}

wait_for_applications_healthy() {
  local timeout_seconds=$1
  local degraded elapsed

  for _ in $(seq 1 "$timeout_seconds"); do
    degraded=$(kubectl --kubeconfig="$TEMP_KUBECONFIG" get applications -n "$ARGOCD_NAMESPACE" \
      -o jsonpath='{.items[?(@.status.health.status!="Healthy" || @.status.sync.status!="Synced")].metadata.name}' 2>/dev/null || true)
    if [[ -z "$degraded" ]]; then
      elapsed=$(($(date +%s) - RECOVERY_START))
      echo "$elapsed"
      return 0
    fi
    sleep 5
  done

  echo "FAIL: Argo CD Applications on the recovered management cluster were not all Healthy/Synced within ${timeout_seconds}s" >&2
  kubectl --kubeconfig="$TEMP_KUBECONFIG" get applications -n "$ARGOCD_NAMESPACE" || true
  exit 1
}

assert_crossplane_ready_on_seed() {
  kubectl --context="$SEED_CONTEXT" rollout status deployment/crossplane -n "$CROSSPLANE_NAMESPACE" --timeout=120s >/dev/null
  log "OK: Crossplane is ready on seed-wus"
}

assert_workload_contexts_reachable() {
  if [[ -z "$WORKLOAD_CONTEXTS" ]]; then
    log "WARN: WORKLOAD_CONTEXTS not set; workload continuity still requires live operator verification"
    return 0
  fi

  for context in $WORKLOAD_CONTEXTS; do
    kubectl --context="$context" get --raw=/readyz >/dev/null
    log "OK: workload cluster API remained reachable for $context"
  done
}

probe_workload_targets_once() {
  if [[ -z "$WORKLOAD_PROBES" ]]; then
    log "WARN: WORKLOAD_PROBES not set; workload continuity checks are limited to cluster API reachability"
    return 0
  fi

  local target name url http_code
  for target in $WORKLOAD_PROBES; do
    if [[ "$target" == *=* ]]; then
      name="${target%%=*}"
      url="${target#*=}"
    else
      name="$target"
      url="$target"
    fi

    http_code=$(curl -k -sS -o /dev/null -w '%{http_code}' "$url" || true)
    if [[ ! "$http_code" =~ ^2[0-9][0-9]$ && ! "$http_code" =~ ^3[0-9][0-9]$ ]]; then
      echo "FAIL: workload probe ${name} returned HTTP ${http_code:-000} from ${url}" >&2
      return 1
    fi
    log "OK: workload probe ${name} returned HTTP ${http_code}"
  done
}

start_workload_probe_monitor() {
  WORKLOAD_MONITOR_STATUS=$(mktemp)
  : >"$WORKLOAD_MONITOR_STATUS"

  if [[ -z "$WORKLOAD_PROBES" ]]; then
    return 0
  fi

  (
    while true; do
      if ! probe_workload_targets_once; then
        echo "failed" >"$WORKLOAD_MONITOR_STATUS"
        exit 1
      fi
      sleep "$WORKLOAD_PROBE_INTERVAL_SECONDS"
    done
  ) &
  WORKLOAD_MONITOR_PID=$!
}

assert_workload_monitor_healthy() {
  if [[ -n "$WORKLOAD_MONITOR_STATUS" && -s "$WORKLOAD_MONITOR_STATUS" ]]; then
    echo "FAIL: workload endpoint monitoring detected a continuity failure during recovery" >&2
    exit 1
  fi
}

require_command kubectl
require_command base64
require_command curl

if [[ ! -f "$CLAIM_FILE" ]]; then
  echo "FAIL: claim file not found: $CLAIM_FILE" >&2
  exit 1
fi

CLAIM_NAME="$(claim_field '{.metadata.name}')"
CLAIM_NAMESPACE="$(claim_field '{.metadata.namespace}')"
CONNECTION_SECRET_NAME="$(claim_field '{.spec.writeConnectionSecretToRef.name}')"
EXPECTED_REPO_URL="$(claim_field '{.metadata.annotations.addons_repo_url}')"
EXPECTED_BOOTSTRAP_PATH="$(claim_field '{.metadata.annotations.bootstrap_repo_path}')"

if [[ -z "$CLAIM_NAMESPACE" ]]; then
  CLAIM_NAMESPACE="$CROSSPLANE_NAMESPACE"
fi

assert_crossplane_ready_on_seed
probe_workload_targets_once
start_workload_probe_monitor

if [[ "$APPLY_CLAIM" == true ]]; then
  validate_claim_placeholders
  log "Applying ${CLAIM_FILE} to ${SEED_CONTEXT}"
  kubectl --context="$SEED_CONTEXT" apply -f "$CLAIM_FILE"
fi

RECOVERY_START=$(date +%s)

log "Waiting for ${CLAIM_NAMESPACE}/${CLAIM_NAME} to become Ready"
wait_for_claim_ready "$CLAIM_NAME" "$CLAIM_NAMESPACE" "$CLAIM_READY_TIMEOUT_SECONDS"

log "Waiting for connection secret ${CLAIM_NAMESPACE}/${CONNECTION_SECRET_NAME}"
wait_for_connection_secret "$CLAIM_NAMESPACE" "$CONNECTION_SECRET_NAME" "$CLUSTER_BOOTSTRAP_TIMEOUT_SECONDS"

wait_for_bootstrap_application "$CLUSTER_BOOTSTRAP_TIMEOUT_SECONDS" "$EXPECTED_REPO_URL" "$EXPECTED_BOOTSTRAP_PATH"
wait_for_leadership_active "$LEASE_READY_TIMEOUT_SECONDS"
assert_workload_monitor_healthy
assert_workload_contexts_reachable

TOTAL_RTO=$(wait_for_applications_healthy "$APPLICATION_HEALTH_TIMEOUT_SECONDS")
assert_workload_monitor_healthy
if [[ "$TOTAL_RTO" -gt "$MAX_RECOVERY_SECONDS" ]]; then
  echo "FAIL: catastrophic recovery RTO ${TOTAL_RTO}s exceeds target ${MAX_RECOVERY_SECONDS}s" >&2
  exit 1
fi

cat <<EOF
Catastrophic recovery validation complete:
  Claim Ready: ${CLAIM_NAMESPACE}/${CLAIM_NAME}
  Recovery kubeconfig Secret: ${CLAIM_NAMESPACE}/${CONNECTION_SECRET_NAME}
  Total Recovery RTO: ${TOTAL_RTO}s
EOF
