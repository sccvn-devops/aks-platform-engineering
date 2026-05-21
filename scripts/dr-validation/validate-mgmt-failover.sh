#!/usr/bin/env bash
# Validates management-plane failover RTO as per US-023.
# Prerequisites:
#   - kubectl configured for both mgmt-we and mgmt-ne contexts
#   - mgmt-cli binary in PATH (built from tools/mgmt-plane-lock/cmd/mgmt-cli)
#   - az CLI authenticated
#
# Usage:
#   ./validate-mgmt-failover.sh
#   ./validate-mgmt-failover.sh --planned-failover
#   ./validate-mgmt-failover.sh --failback

set -euo pipefail

MGMT_WE_CONTEXT="${MGMT_WE_CONTEXT:-mgmt-we-admin}"
MGMT_NE_CONTEXT="${MGMT_NE_CONTEXT:-mgmt-ne-admin}"
STATUS_NAMESPACE="${STATUS_NAMESPACE:-kube-system}"
STATUS_CONFIGMAP="${STATUS_CONFIGMAP:-mgmt-leader-status}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-argocd}"

MAX_FAILOVER_SECONDS=120
LEASE_ACQUIRE_TIMEOUT_SECONDS=65
CONTROLLER_SCALE_TIMEOUT_SECONDS=70
APPLICATION_HEALTH_TIMEOUT_SECONDS=120
FAILBACK_TIMEOUT_SECONDS=120
DRAIN_TIMEOUT_SECONDS="${DRAIN_TIMEOUT_SECONDS:-180s}"

FAILBACK_AFTER_TEST=false
FAILURE_MODE="cluster-outage"
CORDONED_NODES=()

TOTAL_RTO=-1
FAILBACK_RTO=-1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --failback)
      FAILBACK_AFTER_TEST=true
      ;;
    --planned-failover)
      FAILURE_MODE="planned-failover"
      ;;
    *)
      echo "usage: $0 [--failback] [--planned-failover]" >&2
      exit 1
      ;;
  esac
  shift
done

log() {
  echo "[$(date -u '+%H:%M:%S')] $*" >&2
}

restore_cordoned_nodes() {
  if [[ ${#CORDONED_NODES[@]} -eq 0 ]]; then
    return 0
  fi

  for node in "${CORDONED_NODES[@]}"; do
    kubectl --context="$MGMT_WE_CONTEXT" uncordon "$node" >/dev/null 2>&1 || true
  done
}

cleanup() {
  restore_cordoned_nodes
}

trap cleanup EXIT

require_command() {
  local command_name=$1
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "FAIL: required command not found in PATH: $command_name" >&2
    exit 1
  fi
}

jsonpath_or_empty() {
  local context=$1 namespace=$2 kind=$3 name=$4 jsonpath=$5
  kubectl --context="$context" get "$kind" "$name" -n "$namespace" -o "jsonpath=${jsonpath}" 2>/dev/null || true
}

wait_for_pods_gone() {
  local context=$1 namespace=$2 selector=$3 timeout_seconds=$4 description=$5
  local remaining

  for _ in $(seq 1 "$timeout_seconds"); do
    remaining=$(kubectl --context="$context" get pods -n "$namespace" -l "$selector" \
      -o jsonpath='{range .items[*]}{.metadata.name}{" "}{end}' 2>/dev/null || true)
    if [[ -z "$remaining" ]]; then
      log "$description no longer has running pods on $context"
      return 0
    fi
    sleep 1
  done

  echo "FAIL: $description still has pods after ${timeout_seconds}s" >&2
  kubectl --context="$context" get pods -n "$namespace" -l "$selector" || true
  exit 1
}

get_leadership_status() {
  local context=$1
  jsonpath_or_empty "$context" "$STATUS_NAMESPACE" configmap "$STATUS_CONFIGMAP" '{.data.leadershipStatus}'
}

get_lease_blob_url() {
  local context=$1
  jsonpath_or_empty "$context" "$STATUS_NAMESPACE" configmap "$STATUS_CONFIGMAP" '{.data.leaseBlobURL}'
}

assert_leader() {
  local context=$1 expected=$2 actual
  actual=$(get_leadership_status "$context")
  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: expected leadershipStatus=$expected on $context, got ${actual:-<empty>}" >&2
    exit 1
  fi
  log "OK: $context leadershipStatus=$expected"
}

assert_single_active_leader() {
  local we_status ne_status
  we_status=$(get_leadership_status "$MGMT_WE_CONTEXT")
  ne_status=$(get_leadership_status "$MGMT_NE_CONTEXT")

  if [[ "$we_status" == "active" && "$ne_status" == "active" ]]; then
    echo "FAIL: split-brain detected, both management clusters report active leadership" >&2
    exit 1
  fi

  log "Leader state: mgmt-we=${we_status:-unknown}, mgmt-ne=${ne_status:-unknown}"
}

wait_for_leader() {
  local context=$1 expected=$2 timeout_seconds=$3 start_epoch=$4 label=$5
  local status elapsed

  for _ in $(seq 1 "$timeout_seconds"); do
    sleep 1
    status=$(get_leadership_status "$context")
    assert_single_active_leader
    if [[ "$status" == "$expected" ]]; then
      elapsed=$(($(date +%s) - start_epoch))
      log "$label reached leadershipStatus=$expected in ${elapsed}s"
      echo "$elapsed"
      return 0
    fi
  done

  echo "FAIL: $label did not reach leadershipStatus=$expected within ${timeout_seconds}s" >&2
  exit 1
}

wait_for_deployment_ready() {
  local context=$1 namespace=$2 deployment=$3 timeout_seconds=$4 start_epoch=$5 description=$6
  local ready elapsed

  for _ in $(seq 1 "$timeout_seconds"); do
    sleep 1
    ready=$(jsonpath_or_empty "$context" "$namespace" deployment "$deployment" '{.status.readyReplicas}')
    if [[ -n "$ready" && "$ready" =~ ^[0-9]+$ && "$ready" -ge 1 ]]; then
      elapsed=$(($(date +%s) - start_epoch))
      log "$description ready in ${elapsed}s"
      echo "$elapsed"
      return 0
    fi
  done

  echo "FAIL: $description was not ready within ${timeout_seconds}s" >&2
  exit 1
}

wait_for_statefulset_ready() {
  local context=$1 namespace=$2 statefulset=$3 timeout_seconds=$4 start_epoch=$5 description=$6
  local ready elapsed

  for _ in $(seq 1 "$timeout_seconds"); do
    sleep 1
    ready=$(jsonpath_or_empty "$context" "$namespace" statefulset "$statefulset" '{.status.readyReplicas}')
    if [[ -n "$ready" && "$ready" =~ ^[0-9]+$ && "$ready" -ge 1 ]]; then
      elapsed=$(($(date +%s) - start_epoch))
      log "$description ready in ${elapsed}s"
      echo "$elapsed"
      return 0
    fi
  done

  echo "FAIL: $description was not ready within ${timeout_seconds}s" >&2
  exit 1
}

wait_for_applications_healthy() {
  local context=$1 timeout_seconds=$2 start_epoch=$3 degraded elapsed

  for _ in $(seq 1 "$timeout_seconds"); do
    sleep 1
    degraded=$(kubectl --context="$context" get applications -n "$ARGOCD_NAMESPACE" \
      -o jsonpath='{.items[?(@.status.health.status!="Healthy" || @.status.sync.status!="Synced")].metadata.name}' 2>/dev/null || true)
    if [[ -z "$degraded" ]]; then
      elapsed=$(($(date +%s) - start_epoch))
      log "All Argo CD Applications on $context are Healthy/Synced in ${elapsed}s"
      echo "$elapsed"
      return 0
    fi
  done

  echo "FAIL: Argo CD Applications on $context were not all Healthy/Synced within ${timeout_seconds}s" >&2
  kubectl --context="$context" get applications -n "$ARGOCD_NAMESPACE" || true
  exit 1
}

assert_cluster_secret_lease_status() {
  local context=$1 cluster_name=$2 expected=$3
  local actual

  actual=$(kubectl --context="$context" get secret "$cluster_name" -n "$ARGOCD_NAMESPACE" \
    -o jsonpath='{.metadata.labels.lease-status}' 2>/dev/null || true)
  if [[ -z "$actual" ]]; then
    actual=$(kubectl --context="$context" get secrets -n "$ARGOCD_NAMESPACE" \
      -l "argocd.argoproj.io/secret-type=cluster,akuity.io/argo-cd-cluster-name=${cluster_name}" \
      -o jsonpath='{.items[0].metadata.labels.lease-status}' 2>/dev/null || true)
  fi

  if [[ -z "$actual" ]]; then
    log "WARN: no Argo CD cluster Secret found for ${cluster_name} on ${context}; this is expected for in-cluster registration on the active hub"
    return 0
  fi

  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: Argo CD cluster Secret for ${cluster_name} on ${context} has lease-status=${actual}, want ${expected}" >&2
    exit 1
  fi

  log "OK: Argo CD cluster Secret for ${cluster_name} on ${context} has lease-status=$expected"
}

assert_crossplane_ready() {
  local context=$1
  local ready

  ready=$(jsonpath_or_empty "$context" crossplane-system deployment crossplane '{.status.readyReplicas}')
  if [[ "$ready" != "1" ]]; then
    echo "FAIL: Crossplane is not ready on $context (readyReplicas=${ready:-0})" >&2
    exit 1
  fi

  log "OK: Crossplane ready on $context"
}

assert_rollouts_present() {
  local context=$1 count

  count=$(kubectl --context="$context" get applications -n "$ARGOCD_NAMESPACE" \
    -o jsonpath='{range .items[?(@.metadata.name=="argo-rollouts" || @.metadata.name=="addons-argo-rollouts")]}{.metadata.name}{" "}{end}' 2>/dev/null || true)
  if [[ -z "$count" ]]; then
    log "WARN: could not find a dedicated Argo Rollouts Application on $context; validate workload canaries separately"
    return 0
  fi

  log "OK: Argo Rollouts application present on $context: $count"
}

print_summary() {
  cat <<EOF
Validation summary
  Failure mode: ${FAILURE_MODE}
  Lease acquired on mgmt-ne in: ${LEASE_ACQUIRE_SECS}s
  Controllers ready on mgmt-ne in: ${CONTROLLER_READY_SECS}s
  All Applications Healthy/Synced in: ${TOTAL_RTO}s
  Failback RTO: ${FAILBACK_RTO}s
EOF
}

simulate_mgmt_we_failure() {
  local node_lines node

  mapfile -t node_lines < <(kubectl --context="$MGMT_WE_CONTEXT" get nodes -o jsonpath='{range .items[?(@.spec.unschedulable!=true)]}{.metadata.name}{"\n"}{end}')
  if [[ ${#node_lines[@]} -eq 0 ]]; then
    echo "FAIL: no schedulable nodes found on $MGMT_WE_CONTEXT to cordon/drain" >&2
    exit 1
  fi

  for node in "${node_lines[@]}"; do
    [[ -z "$node" ]] && continue
    log "Cordoning node $node on $MGMT_WE_CONTEXT"
    kubectl --context="$MGMT_WE_CONTEXT" cordon "$node" >/dev/null
    CORDONED_NODES+=("$node")
  done

  for node in "${CORDONED_NODES[@]}"; do
    log "Draining node $node on $MGMT_WE_CONTEXT"
    kubectl --context="$MGMT_WE_CONTEXT" drain "$node" \
      --ignore-daemonsets \
      --delete-emptydir-data \
      --force \
      --grace-period=30 \
      --timeout="$DRAIN_TIMEOUT_SECONDS" >/dev/null
  done

  log "Deleting mgmt leader pods on $MGMT_WE_CONTEXT to stop lease renewals"
  kubectl --context="$MGMT_WE_CONTEXT" delete pods -n "$STATUS_NAMESPACE" \
    -l 'app.kubernetes.io/name=mgmt-leader-lease' \
    --ignore-not-found \
    --wait=false >/dev/null

  wait_for_pods_gone "$MGMT_WE_CONTEXT" "$STATUS_NAMESPACE" 'app.kubernetes.io/name=mgmt-leader-lease' 30 "mgmt-leader-lease"
}

planned_failover_to_mgmt_ne() {
  log "Requesting planned failover to mgmt-ne"
  LEASE_BLOB_URL="$LEASE_BLOB_URL" mgmt-cli failback --to mgmt-ne --confirm
}

restore_mgmt_we_capacity() {
  if [[ ${#CORDONED_NODES[@]} -eq 0 ]]; then
    return 0
  fi

  log "Restoring schedulability on mgmt-we after the failover drill"
  restore_cordoned_nodes
  CORDONED_NODES=()
}

require_command kubectl
require_command mgmt-cli

log "Step 1: confirming mgmt-we is active before drill"
assert_leader "$MGMT_WE_CONTEXT" active
assert_leader "$MGMT_NE_CONTEXT" standby
assert_single_active_leader

log "Step 2: discovering the lease blob URL from the active management cluster"
LEASE_BLOB_URL_FROM_CLUSTER=$(get_lease_blob_url "$MGMT_WE_CONTEXT")
LEASE_BLOB_URL="${LEASE_BLOB_URL_FROM_CLUSTER:-${LEASE_BLOB_URL:-}}"
if [[ -z "$LEASE_BLOB_URL" ]]; then
  echo "FAIL: leaseBlobURL missing from ${STATUS_NAMESPACE}/${STATUS_CONFIGMAP} on $MGMT_WE_CONTEXT and LEASE_BLOB_URL env var is unset" >&2
  exit 1
fi
log "Using lease blob URL: $LEASE_BLOB_URL"

log "Step 3: simulating mgmt-we loss"
FAILOVER_START=$(date +%s)
if [[ "$FAILURE_MODE" == "planned-failover" ]]; then
  planned_failover_to_mgmt_ne
else
  simulate_mgmt_we_failure
fi

log "Step 4: waiting for mgmt-ne to acquire the lease"
LEASE_ACQUIRE_SECS=$(wait_for_leader "$MGMT_NE_CONTEXT" active "$LEASE_ACQUIRE_TIMEOUT_SECONDS" "$FAILOVER_START" "mgmt-ne")

log "Step 5: verifying mgmt-we moved to standby"
wait_for_leader "$MGMT_WE_CONTEXT" standby "$LEASE_ACQUIRE_TIMEOUT_SECONDS" "$FAILOVER_START" "mgmt-we" >/dev/null

log "Step 6: waiting for management controllers to scale up on mgmt-ne"
CONTROLLER_READY_SECS=$(wait_for_statefulset_ready "$MGMT_NE_CONTEXT" "$ARGOCD_NAMESPACE" argocd-application-controller "$CONTROLLER_SCALE_TIMEOUT_SECONDS" "$FAILOVER_START" "argocd-application-controller")
wait_for_deployment_ready "$MGMT_NE_CONTEXT" "$ARGOCD_NAMESPACE" argocd-repo-server "$CONTROLLER_SCALE_TIMEOUT_SECONDS" "$FAILOVER_START" "argocd-repo-server" >/dev/null
wait_for_deployment_ready "$MGMT_NE_CONTEXT" "$ARGOCD_NAMESPACE" argocd-server "$CONTROLLER_SCALE_TIMEOUT_SECONDS" "$FAILOVER_START" "argocd-server" >/dev/null
wait_for_deployment_ready "$MGMT_NE_CONTEXT" crossplane-system crossplane "$CONTROLLER_SCALE_TIMEOUT_SECONDS" "$FAILOVER_START" "crossplane" >/dev/null

log "Step 7: checking controller-scaler side effects"
assert_cluster_secret_lease_status "$MGMT_NE_CONTEXT" mgmt-ne active
assert_cluster_secret_lease_status "$MGMT_WE_CONTEXT" mgmt-we standby
assert_crossplane_ready "$MGMT_NE_CONTEXT"
assert_rollouts_present "$MGMT_NE_CONTEXT"

log "Step 8: waiting for Argo CD to report all Applications Healthy/Synced on mgmt-ne"
TOTAL_RTO=$(wait_for_applications_healthy "$MGMT_NE_CONTEXT" "$APPLICATION_HEALTH_TIMEOUT_SECONDS" "$FAILOVER_START")
if [[ "$TOTAL_RTO" -gt "$MAX_FAILOVER_SECONDS" ]]; then
  echo "FAIL: total failover RTO ${TOTAL_RTO}s exceeds target ${MAX_FAILOVER_SECONDS}s" >&2
  exit 1
fi

log "SUCCESS: automatic failover completed within target RTO"
restore_mgmt_we_capacity

if [[ "$FAILBACK_AFTER_TEST" == "true" ]]; then
  log "Step 9: validating operator-driven failback to mgmt-we"
  FAILBACK_START=$(date +%s)
  LEASE_BLOB_URL="$LEASE_BLOB_URL" mgmt-cli failback --to mgmt-we --confirm
  wait_for_leader "$MGMT_WE_CONTEXT" active "$FAILBACK_TIMEOUT_SECONDS" "$FAILBACK_START" "mgmt-we" >/dev/null
  wait_for_leader "$MGMT_NE_CONTEXT" standby "$FAILBACK_TIMEOUT_SECONDS" "$FAILBACK_START" "mgmt-ne" >/dev/null
  assert_single_active_leader
  assert_cluster_secret_lease_status "$MGMT_WE_CONTEXT" mgmt-we active
  assert_cluster_secret_lease_status "$MGMT_NE_CONTEXT" mgmt-ne standby
  FAILBACK_RTO=$(($(date +%s) - FAILBACK_START))
  log "SUCCESS: failback completed in ${FAILBACK_RTO}s"
else
  log "Skipping failback validation. Re-run with --failback to validate operator failback."
fi

print_summary
