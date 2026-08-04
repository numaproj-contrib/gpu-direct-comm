#!/usr/bin/env bash
set -euo pipefail

# E2E test for webhook-whereabouts-numanetwork on a k3d cluster.
# Deploys a Numaflow Pipeline with a gpu-direct edge (connectionType: direct)
# and verifies that ipRange from numaNetwork is assigned to the secondary NIC
# (dummy0) on both vertex Pods, and released on Pipeline deletion.
#
# Prerequisites:
#   - k3d cluster "numaflow-cluster" is running
#   - dranet DaemonSet is deployed
#   - Numaflow is installed
#   - kubectl, docker, k3d, python3 are available

CLUSTER_NAME="${CLUSTER_NAME:-numaflow-cluster}"
WEBHOOK_IMG="${WEBHOOK_IMG:-webhook-whereabouts-numanetwork:dev}"
PIPELINE_NAME="e2e-gpu-direct-pipeline"
NODES=(
  "k3d-${CLUSTER_NAME}-server-0"
  "k3d-${CLUSTER_NAME}-agent-0"
  "k3d-${CLUSTER_NAME}-agent-1"
)

echo "=== Step 1: Create dummy interfaces on each node ==="
for node in "${NODES[@]}"; do
  echo "  -> ${node}"
  docker exec "$node" sh -c "ip link show dummy0 >/dev/null 2>&1 || (ip link add dummy0 type dummy && ip link set up dev dummy0)" || true
done
kubectl get resourceslice -o yaml | grep -A2 'ifName'

echo "=== Step 2: Build and import webhook image ==="
docker build -f Dockerfile.webhook-whereabouts-numanetwork -t "${WEBHOOK_IMG}" .
k3d image import "${WEBHOOK_IMG}" -c "${CLUSTER_NAME}"

echo "=== Step 3: Deploy webhook-whereabouts-numanetwork ==="
kubectl apply -k config/webhook-whereabouts-numanetwork/
kubectl -n kube-system rollout status ds/webhook-whereabouts-numanetwork --timeout=60s

echo "=== Step 4: Patch dranet to webhook mode ==="
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--profile-provider=webhook"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--webhook-url=http://webhook-whereabouts-numanetwork.kube-system.svc:8443"}
]'
kubectl -n kube-system rollout status ds/dranet --timeout=60s

echo "=== Step 5: Deploy Pipeline (NumaNetwork + Pipeline) ==="
kubectl apply -f config/testdata/e2e_ip_assign_local.yaml
kubectl get resourceclaimtemplate -A
kubectl wait --for=condition=ready pod -l "numaflow.numaproj.io/pipeline-name=${PIPELINE_NAME}" --timeout=120s

echo "=== Step 6: Verify IP assignment on dummy0 for both vertex Pods ==="
# Numaflow containers do not include ip command.
# Use nsenter from the node to inspect the Pod's network namespace.
for pod in $(kubectl get pods -l "numaflow.numaproj.io/pipeline-name=${PIPELINE_NAME}" -o name | grep -E 'in-|out-'); do
  pod_name=$(echo "$pod" | sed 's|pod/||')
  node=$(kubectl get "$pod" -o jsonpath='{.spec.nodeName}')
  sandbox=$(docker exec "$node" crictl pods --name "$pod_name" -q | head -1)
  pid=$(docker exec "$node" crictl inspectp "$sandbox" | python3 -c "import sys,json; print(json.load(sys.stdin)['info']['pid'])")
  echo "--- ${pod_name} (node: ${node}) ---"
  docker exec "$node" nsenter -t "$pid" -n ip addr show dummy0
done

echo "=== Step 7: Delete Pipeline and verify IP release ==="
kubectl delete pipeline "${PIPELINE_NAME}"
sleep 5
echo "IPPool allocations after deletion:"
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
echo ""

echo "=== E2E complete ==="
