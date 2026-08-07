#!/usr/bin/env bash
set -euo pipefail

# Full-flow E2E test for gpu-direct-comm (M1–M6 integrated verification).
#
# Deploys a test Pipeline and verifies the entire vertexDomain flow:
#   M1/M4: VertexDomainMutator injects vertexDomain annotation on Pods
#   M3:    vertexDomainController creates DNS records in CoreDNS etcd
#   M5:    DNS resolution of vertexDomain FQDN from a test Pod
#   M6:    Round-robin DNS returns multiple IPs for scaled-out vertex
#   M3:    DNS records are deleted when Pipeline is deleted
#
# Prerequisites (must be deployed before running this script):
#   - Kubernetes cluster is running (k3d or baremetal)
#   - Numaflow is installed
#   - DRANET is deployed with webhook mode (webhook-whereabouts-numanetwork)
#   - DeviceClass is created (dranet-e2e-local or dranet-e2e-baremetal)
#   - whereabouts is installed
#   - cert-manager is installed
#   - gpu-direct-comm controller manager is deployed
#   - CoreDNS etcd backend is deployed (config/coredns-etcd/)
#   See docs/setup-guide.ja.md for full setup instructions.
#
# Usage:
#   ./hack/e2e-full-flow.sh [--env local|baremetal]
#
# Options:
#   --env   Environment type (default: local)
#           local:     k3d cluster with dummy interfaces
#           baremetal: real cluster with SR-IOV VFs

# --- Configuration -----------------------------------------------------------

ENV="local"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)
      ENV="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Usage: $0 [--env local|baremetal]" >&2
      exit 1
      ;;
  esac
done

if [[ "${ENV}" != "local" && "${ENV}" != "baremetal" ]]; then
  echo "Error: --env must be 'local' or 'baremetal', got '${ENV}'" >&2
  exit 1
fi

PIPELINE_NAME="e2e-full-flow-pipeline"
NN_NAME="e2e-full-flow-nn"
TEST_POD="e2e-full-flow-dns-test"
TESTDATA_FILE="config/testdata/e2e_full_flow_${ENV}.yaml"

# vertexDomain FQDN format: <vertex>.<pipeline>.<namespace>.vertexdomain.local
FQDN_IN="in.${PIPELINE_NAME}.default.vertexdomain.local"
FQDN_OUT="out.${PIPELINE_NAME}.default.vertexdomain.local"

LABEL_KEY="gpu-direct-comm.numaproj.io/vertex-domain"
ANNOTATION_KEY="gpu-direct-comm.numaproj.io/vertex-domain-fqdn"

PASS_COUNT=0
FAIL_COUNT=0

pass() { echo "  OK: $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  FAIL: $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

# --- Cleanup ------------------------------------------------------------------

cleanup() {
  echo ""
  echo "=== Cleanup ==="
  kubectl delete pod "${TEST_POD}" --ignore-not-found --wait=false 2>/dev/null || true
  kubectl delete -f "${TESTDATA_FILE}" --ignore-not-found --wait=false 2>/dev/null || true
  echo "  Done"
}
trap cleanup EXIT

# --- Pre-flight checks --------------------------------------------------------

echo "=== Pre-flight: Verify environment (${ENV}) ==="

# Controller manager running
if kubectl get pods -n gpu-direct-comm-system -l control-plane=controller-manager \
    -o jsonpath='{.items[0].status.phase}' 2>/dev/null | grep -q "Running"; then
  pass "controller-manager is Running"
else
  fail "controller-manager is not Running"
  echo "  Deploy the controller first: make docker-build && make deploy"
  exit 1
fi

# CoreDNS etcd running
if kubectl -n kube-system get pod etcd-coredns-0 -o jsonpath='{.status.phase}' 2>/dev/null | grep -q "Running"; then
  pass "etcd-coredns-0 is Running"
else
  fail "etcd-coredns-0 is not Running"
  echo "  Deploy CoreDNS etcd first: kubectl apply -k config/coredns-etcd/"
  exit 1
fi

# DeviceClass exists
DEVICE_CLASS="dranet-e2e-${ENV}"
if kubectl get deviceclass "${DEVICE_CLASS}" >/dev/null 2>&1; then
  pass "DeviceClass ${DEVICE_CLASS} exists"
else
  fail "DeviceClass ${DEVICE_CLASS} not found"
  echo "  Deploy it first: kubectl apply -f config/testdata/e2e_deviceclass_dranet_${ENV}.yaml"
  exit 1
fi

echo ""

# --- Step 1: Deploy Pipeline -------------------------------------------------

echo "=== Step 1: Deploy Pipeline (${TESTDATA_FILE}) ==="
kubectl apply -f "${TESTDATA_FILE}"

echo "  Waiting for vertex Pods to be Ready..."
kubectl wait --for=condition=Ready pod \
  -l "numaflow.numaproj.io/pipeline-name=${PIPELINE_NAME}" \
  --timeout=180s

POD_COUNT=$(kubectl get pods -l "numaflow.numaproj.io/pipeline-name=${PIPELINE_NAME}" \
  --no-headers 2>/dev/null | wc -l)
echo "  ${POD_COUNT} vertex Pod(s) running"

echo ""

# --- Step 2: Verify M1/M4 — vertexDomain annotation --------------------------

echo "=== Step 2: Verify vertexDomain annotation (M1/M4) ==="

for pod in $(kubectl get pods -l "numaflow.numaproj.io/pipeline-name=${PIPELINE_NAME}" -o name); do
  pod_name=$(echo "$pod" | sed 's|pod/||')

  # Check marker label
  label_val=$(kubectl get "$pod" -o jsonpath="{.metadata.labels['gpu-direct-comm\.numaproj\.io/vertex-domain']}" 2>/dev/null || true)
  if [[ "${label_val}" == "true" ]]; then
    pass "${pod_name}: vertex-domain label = true"
  else
    fail "${pod_name}: vertex-domain label missing or != true (got: '${label_val}')"
  fi

  # Check FQDN annotation
  fqdn_val=$(kubectl get "$pod" -o jsonpath="{.metadata.annotations['gpu-direct-comm\.numaproj\.io/vertex-domain-fqdn']}" 2>/dev/null || true)
  if [[ -n "${fqdn_val}" && "${fqdn_val}" == *".vertexdomain.local" ]]; then
    pass "${pod_name}: vertex-domain-fqdn = ${fqdn_val}"
  else
    fail "${pod_name}: vertex-domain-fqdn annotation missing or malformed (got: '${fqdn_val}')"
  fi
done

echo ""

# --- Step 3: Verify M3 — DNS records in etcd ---------------------------------

echo "=== Step 3: Verify DNS records in etcd (M3) ==="

# Wait for controller to reconcile (up to 30s)
ETCD_PREFIX="/skydns/local/vertexdomain/default/${PIPELINE_NAME}/"
for i in $(seq 1 15); do
  ETCD_KEYS=$(kubectl -n kube-system exec etcd-coredns-0 -- \
    etcdctl get --prefix "${ETCD_PREFIX}" --keys-only 2>/dev/null || true)
  if [[ -n "${ETCD_KEYS}" ]]; then
    break
  fi
  echo "  Waiting for DNS records in etcd... (${i}/15)"
  sleep 2
done

if [[ -n "${ETCD_KEYS}" ]]; then
  KEY_COUNT=$(echo "${ETCD_KEYS}" | grep -c "^/" || true)
  pass "etcd has ${KEY_COUNT} DNS record(s) under ${ETCD_PREFIX}"
  echo "${ETCD_KEYS}" | while IFS= read -r key; do
    [[ -z "${key}" ]] && continue
    val=$(kubectl -n kube-system exec etcd-coredns-0 -- etcdctl get "${key}" --print-value-only 2>/dev/null || true)
    echo "    ${key} → ${val}"
  done
else
  fail "No DNS records found in etcd under ${ETCD_PREFIX}"
fi

echo ""

# --- Step 4: Verify M5 — DNS resolution from test Pod ------------------------

echo "=== Step 4: Verify DNS resolution from test Pod (M5) ==="

kubectl run "${TEST_POD}" --restart=Never --image=busybox:1.37 -- sleep 3600
kubectl wait --for=condition=Ready "pod/${TEST_POD}" --timeout=30s

# Resolve "in" vertex FQDN
RESULT_IN=""
for i in $(seq 1 15); do
  RESULT_IN=$(kubectl exec "${TEST_POD}" -- nslookup "${FQDN_IN}" 2>&1 || true)
  if echo "${RESULT_IN}" | grep -q "Address" | head -1 && \
     ! echo "${RESULT_IN}" | grep -q "NXDOMAIN"; then
    break
  fi
  echo "  Waiting for DNS resolution of ${FQDN_IN}... (${i}/15)"
  sleep 2
done

if echo "${RESULT_IN}" | grep -q "192\.168\.141\."; then
  RESOLVED_IP=$(echo "${RESULT_IN}" | grep "Address" | tail -n +2 | head -1 | awk '{print $2}')
  pass "nslookup ${FQDN_IN} → ${RESOLVED_IP}"
else
  fail "nslookup ${FQDN_IN} failed"
  echo "${RESULT_IN}"
fi

echo ""

# --- Step 5: Verify M6 — Round-robin DNS (multiple IPs) ----------------------

echo "=== Step 5: Verify round-robin DNS for scaled vertex (M6) ==="

RESULT_OUT=""
for i in $(seq 1 15); do
  RESULT_OUT=$(kubectl exec "${TEST_POD}" -- nslookup "${FQDN_OUT}" 2>&1 || true)
  if echo "${RESULT_OUT}" | grep -q "192\.168\.141\."; then
    break
  fi
  echo "  Waiting for DNS resolution of ${FQDN_OUT}... (${i}/15)"
  sleep 2
done

# Count IP addresses returned (skip the DNS server address line)
IP_COUNT=$(echo "${RESULT_OUT}" | grep "Address" | tail -n +2 | wc -l)

if [[ "${IP_COUNT}" -ge 2 ]]; then
  pass "nslookup ${FQDN_OUT} returned ${IP_COUNT} IPs (round-robin)"
  echo "${RESULT_OUT}" | grep "Address" | tail -n +2 | while IFS= read -r line; do
    echo "    ${line}"
  done
else
  fail "nslookup ${FQDN_OUT} returned ${IP_COUNT} IP(s), expected >= 2 for round-robin"
  echo "${RESULT_OUT}"
fi

echo ""

# --- Step 6: Verify M3 — DNS record deletion on Pipeline delete ---------------

echo "=== Step 6: Delete Pipeline and verify DNS cleanup (M3) ==="

kubectl delete -f "${TESTDATA_FILE}" --wait=true --timeout=60s

# Wait for controller to clean up DNS records (up to 30s)
for i in $(seq 1 15); do
  REMAINING=$(kubectl -n kube-system exec etcd-coredns-0 -- \
    etcdctl get --prefix "${ETCD_PREFIX}" --keys-only 2>/dev/null || true)
  if [[ -z "${REMAINING}" ]]; then
    break
  fi
  echo "  Waiting for DNS records to be deleted... (${i}/15)"
  sleep 2
done

if [[ -z "${REMAINING}" ]]; then
  pass "All DNS records deleted from etcd after Pipeline deletion"
else
  fail "DNS records still exist in etcd after Pipeline deletion"
  echo "${REMAINING}"
fi

# Verify NXDOMAIN
NXDOMAIN_RESULT=$(kubectl exec "${TEST_POD}" -- nslookup "${FQDN_OUT}" 2>&1 || true)
if echo "${NXDOMAIN_RESULT}" | grep -q "NXDOMAIN"; then
  pass "nslookup ${FQDN_OUT} returns NXDOMAIN after deletion"
else
  fail "nslookup ${FQDN_OUT} did not return NXDOMAIN after deletion"
  echo "${NXDOMAIN_RESULT}"
fi

echo ""

# --- Summary ------------------------------------------------------------------

echo "========================================"
echo "  E2E Full Flow (${ENV}): ${PASS_COUNT} passed, ${FAIL_COUNT} failed"
echo "========================================"

if [[ "${FAIL_COUNT}" -gt 0 ]]; then
  exit 1
fi
