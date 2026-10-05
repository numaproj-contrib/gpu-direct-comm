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

# Section numbers in docs/setup-guide.md, reported alongside each check so a
# failure points straight at the step that did not take effect. The baremetal
# guide inserts "2-6-2. Container registry", shifting every later 2-6-x by one,
# and folds cluster/GPU/DRA/Numaflow into a single section 1.
if [[ "${ENV}" == "local" ]]; then
  SEC_DRA_API="2-2"; SEC_GPU_DRA="-"
  SEC_NUMAFLOW="1-2"; SEC_ISBSVC="1-3"
  SEC_CONTROLLER="2-6-2"; SEC_WEBHOOK_NN="2-6-3"
  SEC_BYODP="2-6-4"; SEC_COREDNS="2-6-5"
else
  SEC_DRA_API="1"; SEC_GPU_DRA="1"
  SEC_NUMAFLOW="1"; SEC_ISBSVC="1"
  SEC_CONTROLLER="2-6-3"; SEC_WEBHOOK_NN="2-6-4"
  SEC_BYODP="2-6-5"; SEC_COREDNS="2-6-6"
fi

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

# Assert a kube-system DaemonSet has every scheduled pod ready. Each caller
# passes the setup-guide section that deploys it, so the three DaemonSets are
# reported under the component that owns them rather than in one lump.
check_ds() {
  local section="$1" ds="$2" desired ready
  desired=$(kubectl -n kube-system get ds "${ds}" -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
  ready=$(kubectl -n kube-system get ds "${ds}" -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
  if [[ "${desired:-0}" -gt 0 && "${desired}" == "${ready}" ]]; then
    pass "[${section}] DaemonSet ${ds}: ${ready}/${desired} ready"
  else
    fail "[${section}] DaemonSet ${ds}: ${ready:-0}/${desired:-0} ready"
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
    pass "[1-1] kubectl context: ${CONTEXT}"
  else
    fail "[1-1] kubectl context: ${CONTEXT} (expected: k3d-numaflow-cluster)"
  fi
  echo ""
fi

# 2. Prerequisites (DRA itself, the GPU DRA driver, Numaflow)
echo "[Prerequisites]"

NUMAFLOW_PODS=$(kubectl get pods -n numaflow-system --no-headers 2>/dev/null | grep -c "Running" || true)
if [[ "${NUMAFLOW_PODS}" -ge 3 ]]; then
  pass "[${SEC_NUMAFLOW}] numaflow-system: ${NUMAFLOW_PODS} pods Running"
else
  fail "[${SEC_NUMAFLOW}] numaflow-system: ${NUMAFLOW_PODS} pods Running (expected >= 3)"
fi

ISBSVC_PHASE=$(kubectl get isbsvc default -o jsonpath='{.status.phase}' 2>/dev/null || true)
if [[ "${ISBSVC_PHASE}" == "Running" ]]; then
  pass "[${SEC_ISBSVC}] ISBSvc default: Running"
else
  fail "[${SEC_ISBSVC}] ISBSvc default: ${ISBSVC_PHASE:-not found} (expected: Running)"
fi

# DRA is a Kubernetes feature, not an installable component: assert the API
# group is served. Without it no driver can publish a ResourceSlice at all.
if kubectl api-resources --api-group=resource.k8s.io 2>/dev/null | grep -q "resourceslices"; then
  pass "[${SEC_DRA_API}] DRA API resource.k8s.io: served"
else
  fail "[${SEC_DRA_API}] DRA API resource.k8s.io: not served (DynamicResourceAllocation feature gate disabled?)"
fi

# GPU DRA driver (baremetal only; the local k3d cluster has no GPU).
# DRANET is the NIC-side DRA driver and is checked under [DRA] below.
if [[ "${ENV}" == "baremetal" ]]; then
  GPU_DRA_PODS=$(kubectl -n nvidia-dra-driver-gpu get pods --no-headers 2>/dev/null || true)
  GPU_DRA_CTRL=$(echo "${GPU_DRA_PODS}" | grep "controller" | grep -c "Running" || true)
  GPU_DRA_PLUGIN=$(echo "${GPU_DRA_PODS}" | grep "kubelet-plugin" | grep -c "Running" || true)
  if [[ "${GPU_DRA_CTRL}" -ge 1 && "${GPU_DRA_PLUGIN}" -ge 1 ]]; then
    pass "[${SEC_GPU_DRA}] nvidia-dra-driver-gpu: controller ${GPU_DRA_CTRL} / kubelet-plugin ${GPU_DRA_PLUGIN} Running"
  else
    fail "[${SEC_GPU_DRA}] nvidia-dra-driver-gpu: controller ${GPU_DRA_CTRL} / kubelet-plugin ${GPU_DRA_PLUGIN} Running (expected >= 1 each)"
  fi
fi

echo ""
# 3. DRA device supply (NIC driver, DeviceClass, published devices)
echo "[DRA]"
# DRANET is the NIC-side DRA driver, installed in setup-guide 2-2.
check_ds "2-2" dranet

# DeviceClass next: the device checks below assert the attribute this
# DeviceClass selects on, so they carry no meaning without it.
if kubectl get deviceclass "${DEVICECLASS_NAME}" >/dev/null 2>&1; then
  pass "[2-3] DeviceClass ${DEVICECLASS_NAME} exists"
else
  fail "[2-3] DeviceClass ${DEVICECLASS_NAME} not found"
fi

# Then the devices it is supposed to select.
if [[ "${ENV}" == "local" ]]; then
  # local uses dummy interfaces, not SR-IOV VFs, and has no GPU: assert only
  # that DRANET publishes devices at all.
  RS_COUNT=$(kubectl get resourceslice --no-headers 2>/dev/null | wc -l || true)
  if [[ "${RS_COUNT}" -gt 0 ]]; then
    pass "[2-1 ~ 2-3] ResourceSlice objects: ${RS_COUNT}"
  else
    fail "[2-1 ~ 2-3] ResourceSlice objects: 0 (DRANET not publishing devices)"
  fi
else
  # baremetal needs a GPU and an SR-IOV VF on the *same* node, so assert per
  # node rather than cluster-wide: a non-zero count alone passes with only PFs
  # and tunl0 present. The VF predicate matches the attribute the DeviceClass
  # selects on (dra.net/isSriovVf); ifName and productName are shown for
  # readability. Nodes where dranet or the GPU kubelet-plugin is down publish no
  # ResourceSlice at all and are covered by the DaemonSet / pod checks instead.
  VF_LINES=$(kubectl get resourceslice -o jsonpath='{range .items[?(@.spec.driver=="dra.net")]}{.spec.nodeName}{"\t"}{range .spec.devices[?(@.attributes.dra\.net/isSriovVf.bool==true)]}{.attributes.dra\.net/ifName.string}{" "}{end}{"\n"}{end}' 2>/dev/null || true)
  GPU_LINES=$(kubectl get resourceslice -o jsonpath='{range .items[?(@.spec.driver=="gpu.nvidia.com")]}{.spec.nodeName}{"\t"}{range .spec.devices[?(@.attributes.type.string=="gpu")]}{.name}{"("}{.attributes.productName.string}{") "}{end}{"\n"}{end}' 2>/dev/null || true)
  NODES=$(printf '%s\n%s\n' "${VF_LINES}" "${GPU_LINES}" | cut -f1 | grep -v '^$' | sort -u || true)
  if [[ -z "${NODES}" ]]; then
    fail "[2-1 ~ 2-3] ResourceSlice: no node publishes dra.net or gpu.nvidia.com devices"
  else
    for node in ${NODES}; do
      vfs=$(printf '%s\n' "${VF_LINES}" | awk -F'\t' -v n="${node}" '$1==n{print $2}')
      gpus=$(printf '%s\n' "${GPU_LINES}" | awk -F'\t' -v n="${node}" '$1==n{print $2}')
      vfs="${vfs% }"
      gpus="${gpus% }"
      if [[ -n "${vfs}" && -n "${gpus}" ]]; then
        pass "[2-1 ~ 2-3] ResourceSlice ${node}: GPU ${gpus} / VF ${vfs}"
      else
        fail "[2-1 ~ 2-3] ResourceSlice ${node}: GPU ${gpus:-none} / VF ${vfs:-none (dra.net/isSriovVf=true not published)}"
      fi
    done
  fi
fi
echo ""

# 4. whereabouts
echo "[whereabouts]"
check_ds "2-4" whereabouts
WA_CONF=$(kubectl -n kube-system exec ds/whereabouts -- cat /host/etc/cni/net.d/whereabouts.d/whereabouts.conf 2>/dev/null || true)
if echo "${WA_CONF}" | grep -q "kubeconfig"; then
  pass "[2-4] whereabouts config: kubeconfig field present"
else
  fail "[2-4] whereabouts config: kubeconfig field not found"
fi
echo ""

# 5. cert-manager
echo "[cert-manager]"
CM_PODS=$(kubectl get pods -n cert-manager --no-headers 2>/dev/null | grep -c "Running" || true)
if [[ "${CM_PODS}" -ge 3 ]]; then
  pass "[2-5] cert-manager: ${CM_PODS} pods Running"
else
  fail "[2-5] cert-manager: ${CM_PODS} pods Running (expected >= 3)"
fi
echo ""

# 6. gpu-direct-comm
echo "[gpu-direct-comm]"

# CRD
if kubectl get crd numanetworks.numaflow.numaproj.io >/dev/null 2>&1; then
  pass "[2-6-1] NumaNetwork CRD registered"
else
  fail "[2-6-1] NumaNetwork CRD not found"
fi

# Controller manager
CM_STATUS=$(kubectl get pods -n gpu-direct-comm-system --no-headers 2>/dev/null | grep "controller-manager" | grep -c "Running" || true)
if [[ "${CM_STATUS}" -ge 1 ]]; then
  pass "[${SEC_CONTROLLER}] controller-manager: Running"
else
  fail "[${SEC_CONTROLLER}] controller-manager: not Running"
fi

check_ds "${SEC_WEBHOOK_NN}" webhook-whereabouts-numanetwork

# BYODP wiring: this patches the dranet DaemonSet, but it is a gpu-direct-comm
# step (setup-guide 2-6-x) that delegates IPAM to webhook-whereabouts-numanetwork.
DRANET_ARGS=$(kubectl -n kube-system get ds dranet -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || true)
if echo "${DRANET_ARGS}" | grep -q "profile-provider=webhook"; then
  pass "[${SEC_BYODP}] DRANET BYODP webhook: --profile-provider=webhook configured"
else
  fail "[${SEC_BYODP}] DRANET BYODP webhook: --profile-provider=webhook not found in args"
fi
echo ""

# 7. CoreDNS etcd backend
echo "[CoreDNS etcd]"
ETCD_HEALTH=$(kubectl -n kube-system exec etcd-coredns-0 -- etcdctl endpoint health 2>/dev/null || true)
if echo "${ETCD_HEALTH}" | grep -q "is healthy"; then
  pass "[${SEC_COREDNS}] etcd-coredns-0: healthy"
else
  fail "[${SEC_COREDNS}] etcd-coredns-0: not healthy or not found"
fi

# Query a non-existent name in the zone.  If the zone is loaded CoreDNS
# returns NXDOMAIN; if not, it returns SERVFAIL.
kubectl delete pod verify-setup-dns-check --ignore-not-found --wait=true 2>/dev/null || true
DNS_CHECK=$(kubectl run verify-setup-dns-check --rm -i --restart=Never \
  --image=busybox:1.37 -- nslookup dummy.vertexdomain.local 2>&1 || true)
if echo "${DNS_CHECK}" | grep -q "NXDOMAIN"; then
  pass "[${SEC_COREDNS}] CoreDNS: vertexdomain.local zone active (NXDOMAIN)"
else
  fail "[${SEC_COREDNS}] CoreDNS: vertexdomain.local zone not responding (expected NXDOMAIN)"
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
