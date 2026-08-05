#!/usr/bin/env bash
set -euo pipefail

# E2E test for CoreDNS etcd plugin on a k3d cluster.
# Deploys a dedicated etcd + CoreDNS custom config, registers DNS records
# via etcdctl, and verifies name resolution from a test Pod.
#
# Prerequisites:
#   - k3d cluster "numaflow-cluster" is running
#   - kubectl is available

CLUSTER_NAME="${CLUSTER_NAME:-numaflow-cluster}"
TEST_POD="dns-e2e-test"
TEST_FQDN="vertex-in.pipeline1.default.vertexdomain.local"
TEST_IP="192.168.140.10"
ETCD_KEY="/skydns/local/vertexdomain/default/pipeline1/vertex-in"

cleanup() {
  echo "=== Cleanup ==="
  kubectl delete pod "${TEST_POD}" --ignore-not-found --wait=false 2>/dev/null || true
  kubectl -n kube-system exec etcd-coredns-0 -- etcdctl del "${ETCD_KEY}" 2>/dev/null || true
}
trap cleanup EXIT

echo "=== Step 1: Deploy etcd and CoreDNS custom config ==="
kubectl apply -k config/coredns-etcd/
kubectl -n kube-system wait --for=condition=Ready pod/etcd-coredns-0 --timeout=60s

echo "=== Step 2: Restart CoreDNS to load vertexdomain.local zone ==="
kubectl -n kube-system rollout restart deployment/coredns
kubectl -n kube-system rollout status deployment/coredns --timeout=60s

echo "=== Step 3: Verify CoreDNS loaded vertexdomain.local zone ==="
sleep 3
if kubectl -n kube-system logs -l k8s-app=kube-dns --tail=30 | grep -q "vertexdomain.local.:53"; then
  echo "  OK: vertexdomain.local zone loaded"
else
  echo "  FAIL: vertexdomain.local zone not found in CoreDNS logs"
  kubectl -n kube-system logs -l k8s-app=kube-dns --tail=30
  exit 1
fi

echo "=== Step 4: Verify etcd health ==="
kubectl -n kube-system exec etcd-coredns-0 -- etcdctl endpoint health

echo "=== Step 5: Register test A record ==="
kubectl -n kube-system exec etcd-coredns-0 -- etcdctl put \
  "${ETCD_KEY}" \
  "{\"host\":\"${TEST_IP}\"}"
echo "  Registered: ${TEST_FQDN} -> ${TEST_IP}"

echo "=== Step 6: Start test Pod ==="
kubectl run "${TEST_POD}" --restart=Never --image=busybox:1.37 -- sleep 3600
kubectl wait --for=condition=Ready "pod/${TEST_POD}" --timeout=30s

echo "=== Step 7: Verify DNS resolution (expect ${TEST_IP}) ==="
RESULT=$(kubectl exec "${TEST_POD}" -- nslookup "${TEST_FQDN}" 2>&1)
echo "${RESULT}"
if echo "${RESULT}" | grep -q "${TEST_IP}"; then
  echo "  OK: DNS resolution returned ${TEST_IP}"
else
  echo "  FAIL: expected ${TEST_IP} in DNS response"
  exit 1
fi

echo "=== Step 8: Delete record and verify NXDOMAIN ==="
kubectl -n kube-system exec etcd-coredns-0 -- etcdctl del "${ETCD_KEY}"
RESULT=$(kubectl exec "${TEST_POD}" -- nslookup "${TEST_FQDN}" 2>&1 || true)
echo "${RESULT}"
if echo "${RESULT}" | grep -q "NXDOMAIN"; then
  echo "  OK: DNS resolution returned NXDOMAIN after deletion"
else
  echo "  FAIL: expected NXDOMAIN after record deletion"
  exit 1
fi

echo "=== E2E complete ==="
