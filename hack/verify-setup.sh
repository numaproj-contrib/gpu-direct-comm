#!/usr/bin/env bash
set -euo pipefail

# Verify that the gpu-direct-comm development environment is fully operational.
#
# Auto-detects environment type (local k3d / baremetal) from the current
# kubectl context. Override with --env if auto-detection is wrong.
#
# Usage:
#   ./hack/verify-setup.sh [--env local|baremetal]

# --- Configuration -----------------------------------------------------------

ENV=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env) ENV="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

# Auto-detect environment from kubectl context
if [[ -z "${ENV}" ]]; then
  CONTEXT=$(kubectl config current-context 2>/dev/null || true)
  if [[ "${CONTEXT}" == k3d-* ]]; then
    ENV="local"
  else
    ENV="baremetal"
  fi
  echo "Auto-detected environment: ${ENV} (context: ${CONTEXT})"
fi

if [[ "${ENV}" != "local" && "${ENV}" != "baremetal" ]]; then
  echo "Error: --env must be 'local' or 'baremetal', got '${ENV}'" >&2
  exit 1
fi

DEVICECLASS_NAME="dranet-e2e-${ENV}"

# --- Helpers -----------------------------------------------------------------

PASS_COUNT=0
FAIL_COUNT=0

pass() { echo "  ✅ $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  ❌ $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

check() {
  local description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    pass "${description}"
  else
    fail "${description}"
  fi
}

# --- Checks ------------------------------------------------------------------

echo ""
echo "=== gpu-direct-comm setup verification (${ENV}) ==="
echo ""

# 1. Cluster context (local only)
if [[ "${ENV}" == "local" ]]; then
  echo "[Cluster]"
  CONTEXT=$(kubectl config current-context 2>/dev/null || true)
  if [[ "${CONTEXT}" == "k3d-numaflow-cluster" ]]; then
    pass "kubectl context: ${CONTEXT}"
  else
    fail "kubectl context: ${CONTEXT} (expected: k3d-numaflow-cluster)"
  fi
  echo ""
fi

# 2. Numaflow
echo "[Numaflow]"
NUMAFLOW_PODS=$(kubectl get pods -n numaflow-system --no-headers 2>/dev/null | grep -c "Running" || true)
if [[ "${NUMAFLOW_PODS}" -ge 3 ]]; then
  pass "numaflow-system: ${NUMAFLOW_PODS} pods Running"
else
  fail "numaflow-system: ${NUMAFLOW_PODS} pods Running (expected >= 3)"
fi

# ISBSvc
ISBSVC_PHASE=$(kubectl get isbsvc default -o jsonpath='{.status.phase}' 2>/dev/null || true)
if [[ "${ISBSVC_PHASE}" == "Running" ]]; then
  pass "ISBSvc default: Running"
else
  fail "ISBSvc default: ${ISBSVC_PHASE:-not found} (expected: Running)"
fi
echo ""

# 3. DRANET
echo "[DRANET]"
RS_COUNT=$(kubectl get resourceslice --no-headers 2>/dev/null | wc -l || true)
if [[ "${RS_COUNT}" -gt 0 ]]; then
  pass "ResourceSlice objects: ${RS_COUNT}"
else
  fail "ResourceSlice objects: 0 (DRANET not publishing devices)"
fi

# DeviceClass
if kubectl get deviceclass "${DEVICECLASS_NAME}" >/dev/null 2>&1; then
  pass "DeviceClass ${DEVICECLASS_NAME} exists"
else
  fail "DeviceClass ${DEVICECLASS_NAME} not found"
fi

# BYODP webhook args
DRANET_ARGS=$(kubectl -n kube-system get ds dranet -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || true)
if echo "${DRANET_ARGS}" | grep -q "profile-provider=webhook"; then
  pass "DRANET BYODP webhook: --profile-provider=webhook configured"
else
  fail "DRANET BYODP webhook: --profile-provider=webhook not found in args"
fi
echo ""

# 4. whereabouts
echo "[whereabouts]"
WA_CONF=$(kubectl -n kube-system exec ds/whereabouts -- cat /host/etc/cni/net.d/whereabouts.d/whereabouts.conf 2>/dev/null || true)
if echo "${WA_CONF}" | grep -q "kubeconfig"; then
  pass "whereabouts config: kubeconfig field present"
else
  fail "whereabouts config: kubeconfig field not found"
fi
echo ""

# 5. cert-manager
echo "[cert-manager]"
CM_PODS=$(kubectl get pods -n cert-manager --no-headers 2>/dev/null | grep -c "Running" || true)
if [[ "${CM_PODS}" -ge 3 ]]; then
  pass "cert-manager: ${CM_PODS} pods Running"
else
  fail "cert-manager: ${CM_PODS} pods Running (expected >= 3)"
fi
echo ""

# 6. gpu-direct-comm
echo "[gpu-direct-comm]"

# CRD
if kubectl get crd numanetworks.numaflow.numaproj.io >/dev/null 2>&1; then
  pass "NumaNetwork CRD registered"
else
  fail "NumaNetwork CRD not found"
fi

# Controller manager
CM_STATUS=$(kubectl get pods -n gpu-direct-comm-system --no-headers 2>/dev/null | grep "controller-manager" | grep -c "Running" || true)
if [[ "${CM_STATUS}" -ge 1 ]]; then
  pass "controller-manager: Running"
else
  fail "controller-manager: not Running"
fi

# DaemonSets
for ds in whereabouts dranet webhook-whereabouts-numanetwork; do
  DESIRED=$(kubectl -n kube-system get ds "${ds}" -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
  READY=$(kubectl -n kube-system get ds "${ds}" -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
  if [[ "${DESIRED}" -gt 0 && "${DESIRED}" == "${READY}" ]]; then
    pass "DaemonSet ${ds}: ${READY}/${DESIRED} ready"
  else
    fail "DaemonSet ${ds}: ${READY}/${DESIRED} ready"
  fi
done
echo ""

# 7. CoreDNS etcd backend
echo "[CoreDNS etcd]"
ETCD_HEALTH=$(kubectl -n kube-system exec etcd-coredns-0 -- etcdctl endpoint health 2>/dev/null || true)
if echo "${ETCD_HEALTH}" | grep -q "is healthy"; then
  pass "etcd-coredns-0: healthy"
else
  fail "etcd-coredns-0: not healthy or not found"
fi

# Query a non-existent name in the zone.  If the zone is loaded CoreDNS
# returns NXDOMAIN; if not, it returns SERVFAIL.
DNS_CHECK=$(kubectl run dns-check-$$ --rm -i --restart=Never \
  --image=busybox:1.37 -- nslookup dummy.vertexdomain.local 2>&1 || true)
if echo "${DNS_CHECK}" | grep -q "NXDOMAIN"; then
  pass "CoreDNS: vertexdomain.local zone active (NXDOMAIN)"
else
  fail "CoreDNS: vertexdomain.local zone not responding (expected NXDOMAIN)"
fi
echo ""

# --- Summary -----------------------------------------------------------------

TOTAL=$((PASS_COUNT + FAIL_COUNT))
echo "=== Summary: ${PASS_COUNT}/${TOTAL} passed ==="
if [[ "${FAIL_COUNT}" -gt 0 ]]; then
  echo ""
  echo "Some checks failed. See docs/setup-guide.ja.md for setup instructions."
  exit 1
fi
