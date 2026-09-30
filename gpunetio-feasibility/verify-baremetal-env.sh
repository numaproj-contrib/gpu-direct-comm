#!/usr/bin/env bash
set -u

# pipefail is deliberately NOT set: `grep -q` exits on the first match and can
# make the producing command die of SIGPIPE, which pipefail would then report
# as a failed pipeline. That turns a successful match into a false negative.

# Baremetal environment verification for GPUNetIO feasibility PoC (G3-M1).
#
# Verifies that the host-side setup established in M1-1 is still in effect.
# Most of that setup is volatile: ACS/IOMMU disabling, gdrdrv loading and
# RoCE link-up are written to the running kernel or to PCI config space and are
# lost on reboot. Run this on every cluster node before Pod-based testing so
# that a red stage can be attributed to the Pod wiring, not to a host setting
# that silently reverted.
#
# Run this ON the node itself (not from the control plane):
#   ./verify-baremetal-env.sh
#   ssh cobalt04 'bash -s' < gpunetio-feasibility/verify-baremetal-env.sh
#
# Exit code: 0 if all checks pass, 1 if any check fails.
#
# Reference: ~/sesame-brain/wiki/gpu-direct-comm/gpunetio-feasibility/ENVIRONMENT_SETUP.md

PASS_COUNT=0
FAIL_COUNT=0
WARN_COUNT=0

pass() { echo "  [PASS] $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "  [FAIL] $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }
warn() { echo "  [WARN] $1"; WARN_COUNT=$((WARN_COUNT + 1)); }
info() { echo "  [INFO] $1"; }

# sudo is needed for lspci -vvv (ACSCtl). Skip that check rather than blocking
# on a password prompt in a non-interactive run.
if [[ "$(id -u)" -eq 0 ]]; then
  SUDO=""
  HAS_SUDO=1
elif sudo -n true 2>/dev/null; then
  SUDO="sudo"
  HAS_SUDO=1
else
  SUDO=""
  HAS_SUDO=0
fi

echo "========================================"
echo "  Baremetal env check: $(hostname)"
echo "========================================"
echo ""

# --- 1.1 DOCA-Host ------------------------------------------------------------

echo "=== 1.1 DOCA-Host ==="
DOCA_VER=$(dpkg-query -W -f='${Version}' doca-all 2>/dev/null)
if [[ -n "${DOCA_VER}" ]]; then
  pass "doca-all installed (${DOCA_VER})"
else
  fail "doca-all is not installed (see ENVIRONMENT_SETUP.md 1.1)"
fi

echo ""

# --- 1.2 GPUNetIO SDK ---------------------------------------------------------

echo "=== 1.2 GPUNetIO SDK ==="
GPUNETIO_VER=$(dpkg-query -W -f='${Version}' doca-sdk-gpunetio 2>/dev/null)
if [[ -n "${GPUNETIO_VER}" ]]; then
  pass "doca-sdk-gpunetio installed (${GPUNETIO_VER})"
else
  fail "doca-sdk-gpunetio is not installed (see ENVIRONMENT_SETUP.md 1.2)"
fi

echo ""

# --- 1.3 Topology (recorded, not judged) --------------------------------------
#
# GPU-NIC PCIe proximity (PIX/NODE/SYS) cannot be judged pass/fail here: it is
# a property of the chassis. It is recorded because it explains bandwidth
# differences later, and because M1-2 Task 5 compares it against what DRA
# actually allocates inside the Pod.

echo "=== 1.3 PCIe Topology (INFO) ==="
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi topo -m 2>/dev/null | sed 's/^/  /'
else
  warn "nvidia-smi not found; topology not recorded"
fi

echo ""
echo "=== 1.3 IB device -> netdev mapping (INFO) ==="
IB_COUNT=0
for ib in /sys/class/infiniband/*; do
  [[ -e "${ib}" ]] || continue
  IB_COUNT=$((IB_COUNT + 1))
  dev=$(basename "${ib}")
  net=$(ls "${ib}/device/net/" 2>/dev/null | head -1)
  bdf=$(basename "$(readlink -f "${ib}/device")")
  if lspci -s "${bdf}" 2>/dev/null | grep -q "Virtual Function"; then
    kind="VF"
  else
    kind="PF"
  fi
  info "${dev} -> ${net:-<none>} (PCIe: ${bdf}, ${kind})"
done
if [[ "${IB_COUNT}" -eq 0 ]]; then
  fail "no InfiniBand/RoCE device found under /sys/class/infiniband"
fi

echo ""

# --- 1.4 Port link state ------------------------------------------------------
#
# Volatile: `ip link set <iface> up` does not survive a reboot unless the
# netplan config from ENVIRONMENT_SETUP.md 1.4 is in place.

echo "=== 1.4 Port link state ==="
ACTIVE_PORTS=0
for ib in /sys/class/infiniband/*; do
  [[ -e "${ib}" ]] || continue
  dev=$(basename "${ib}")
  for port in "${ib}"/ports/*; do
    [[ -e "${port}" ]] || continue
    pnum=$(basename "${port}")
    state=$(cat "${port}/state" 2>/dev/null)
    link=$(cat "${port}/link_layer" 2>/dev/null)
    if [[ "${state}" == *ACTIVE* ]]; then
      pass "${dev}:${pnum} state=${state} link_layer=${link}"
      ACTIVE_PORTS=$((ACTIVE_PORTS + 1))
    else
      info "${dev}:${pnum} state=${state} link_layer=${link}"
    fi
  done
done
if [[ "${ACTIVE_PORTS}" -eq 0 ]]; then
  fail "no port is PORT_ACTIVE; bring the RoCE interface up (ENVIRONMENT_SETUP.md 1.4)"
fi

echo ""

# --- 1.5 ACS ------------------------------------------------------------------
#
# Volatile: setpci writes to PCI config space and is reset on reboot.
# The bridges to check differ per node, so they are derived from the actual
# PCIe tree: every bridge on the path shared by a GPU and an RDMA NIC is where
# ACS would redirect peer-to-peer traffic up to the root complex and break
# GPUDirect RDMA.

echo "=== 1.5 ACS (GPU-NIC common upstream bridges) ==="

# Print the BDFs of every PCI bridge above the given sysfs device path.
pci_ancestors() {
  local p b
  p=$(dirname "$(readlink -f "$1")")
  while [[ "${p}" != "/sys/devices" && "${p}" != "/" ]]; do
    b=$(basename "${p}")
    if [[ "${b}" =~ ^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-9a-f]$ ]]; then
      echo "${b}"
    fi
    p=$(dirname "${p}")
  done
}

GPU_PATHS=()
while IFS= read -r bdf; do
  [[ -n "${bdf}" ]] && GPU_PATHS+=("/sys/bus/pci/devices/${bdf}")
done < <(lspci -D -n 2>/dev/null | awk '$2 ~ /^03[0-9a-f][0-9a-f]:/ {print $1}')

NIC_PATHS=()
for ib in /sys/class/infiniband/*; do
  [[ -e "${ib}" ]] || continue
  NIC_PATHS+=("${ib}/device")
done

BRIDGES=""
if [[ ${#GPU_PATHS[@]} -eq 0 || ${#NIC_PATHS[@]} -eq 0 ]]; then
  warn "GPU or RDMA NIC not found; skipping ACS check"
else
  for gpu in "${GPU_PATHS[@]}"; do
    gpu_anc=$(pci_ancestors "${gpu}" | sort -u)
    for nic in "${NIC_PATHS[@]}"; do
      nic_anc=$(pci_ancestors "${nic}" | sort -u)
      common=$(comm -12 <(echo "${gpu_anc}") <(echo "${nic_anc}"))
      BRIDGES="${BRIDGES}"$'\n'"${common}"
    done
  done
  BRIDGES=$(echo "${BRIDGES}" | grep -v '^$' | sort -u)
fi

if [[ -n "${BRIDGES}" ]]; then
  if [[ "${HAS_SUDO}" -eq 0 ]]; then
    warn "passwordless sudo unavailable; ACSCtl not readable (re-run with sudo)"
  else
    while IFS= read -r bridge; do
      [[ -n "${bridge}" ]] || continue
      acsctl=$(${SUDO} lspci -s "${bridge}" -vvv 2>/dev/null | grep -m1 "ACSCtl:")
      if [[ -z "${acsctl}" ]]; then
        pass "${bridge}: no ACS capability (nothing to disable)"
      elif echo "${acsctl}" | grep -q "+"; then
        fail "${bridge}: ACS enabled -${acsctl#*ACSCtl:} (ENVIRONMENT_SETUP.md 1.5)"
      else
        pass "${bridge}: ACS disabled"
      fi
    done <<< "${BRIDGES}"
  fi
elif [[ ${#GPU_PATHS[@]} -gt 0 && ${#NIC_PATHS[@]} -gt 0 ]]; then
  warn "GPU and RDMA NIC share no upstream bridge (different root complexes)"
fi

echo ""

# --- 1.5 IOMMU ----------------------------------------------------------------

echo "=== 1.5 IOMMU ==="
if grep -qE '(^|[[:space:]])(intel_|amd_)?iommu=(off|disabled)' /proc/cmdline; then
  pass "IOMMU disabled via kernel cmdline"
elif grep -qE '(^|[[:space:]])(intel_|amd_)?iommu=' /proc/cmdline; then
  fail "IOMMU kernel param present but not off: $(tr ' ' '\n' < /proc/cmdline | grep -i iommu | tr '\n' ' ')"
elif [[ -n "$(ls -A /sys/class/iommu 2>/dev/null)" ]]; then
  fail "IOMMU is active ($(ls /sys/class/iommu | tr '\n' ' ')) with no iommu=off in cmdline"
else
  pass "IOMMU not active (no iommu kernel param, /sys/class/iommu empty)"
fi

echo ""

# --- 1.6 CUDA -----------------------------------------------------------------

echo "=== 1.6 CUDA dependency ==="
# A non-interactive ssh session does not source /etc/profile.d, so fall back to
# the default install path before declaring CUDA missing.
NVCC=$(command -v nvcc 2>/dev/null || true)
if [[ -z "${NVCC}" && -x /usr/local/cuda/bin/nvcc ]]; then
  NVCC=/usr/local/cuda/bin/nvcc
fi
CUDA_REL=""
if [[ -n "${NVCC}" ]]; then
  CUDA_REL=$("${NVCC}" --version 2>/dev/null | grep -oP 'release \K[0-9.]+')
fi
if [[ -n "${CUDA_REL}" ]]; then
  pass "nvcc release ${CUDA_REL} (${NVCC})"
else
  fail "nvcc not found (check PATH via /etc/profile.d/cuda-paths.sh)"
fi

PM_STATES=$(nvidia-smi --query-gpu=persistence_mode --format=csv,noheader 2>/dev/null)
if [[ -z "${PM_STATES}" ]]; then
  fail "nvidia-smi could not query persistence_mode"
elif echo "${PM_STATES}" | grep -qi "Disabled"; then
  fail "GPU persistence mode disabled on some GPU (sudo nvidia-smi -pm 1)"
else
  pass "GPU persistence mode enabled on all GPUs"
fi

echo ""

# --- 1.6 GDRCopy --------------------------------------------------------------
#
# Volatile: gdrdrv is loaded by insmod.sh and is gone after a reboot unless
# registered under /etc/modules-load.d/.

echo "=== 1.6 GDRCopy ==="
if lsmod 2>/dev/null | grep -q '^gdrdrv'; then
  pass "gdrdrv loaded"
else
  fail "gdrdrv not loaded (sudo /opt/mellanox/gdrcopy/insmod.sh)"
fi

if ldconfig -p 2>/dev/null | grep -q libgdrapi; then
  pass "libgdrapi found by ldconfig"
else
  fail "libgdrapi not found (check /etc/ld.so.conf.d/gdrcopy.conf)"
fi

echo ""

# --- 1.6 GPU Memory Mapping: Using dmabuf --------------------------------------
#
# ENVIRONMENT_SETUP.md "Using dmabuf" lists four preconditions (kernel >= 6.2,
# libibverbs dmabuf support, CUDA Toolkit version [checked above], nvidia
# driver = Open Kernel Module). Evaluate all of them as pass/fail here instead
# of the previous info-only printout, mirroring the doc's four-condition list.

echo "=== 1.6 GPU Memory Mapping: dmabuf ==="
KVER=$(uname -r)
KVER_MAJMIN=$(echo "${KVER}" | grep -oP '^[0-9]+\.[0-9]+')
if [[ -n "${KVER_MAJMIN}" && "$(printf '%s\n%s\n' "6.2" "${KVER_MAJMIN}" | sort -V | head -1)" == "6.2" ]]; then
  pass "kernel ${KVER} >= 6.2"
else
  fail "kernel ${KVER} < 6.2 (dmabuf path unavailable)"
fi

IBVERBS_SO=$(ldconfig -p 2>/dev/null | grep -m1 'libibverbs.so.1' | awk '{print $NF}')
if [[ -n "${IBVERBS_SO}" ]] && nm -D "${IBVERBS_SO}" 2>/dev/null | grep -q ibv_reg_dmabuf_mr; then
  pass "libibverbs exports ibv_reg_dmabuf_mr (dmabuf path available)"
else
  warn "ibv_reg_dmabuf_mr not exported; only the nvidia-peermem path is usable"
fi

NVIDIA_VER_LINE=$(head -1 /proc/driver/nvidia/version 2>/dev/null || true)
if echo "${NVIDIA_VER_LINE}" | grep -q "Open Kernel Module"; then
  pass "nvidia driver is Open Kernel Module"
else
  fail "nvidia driver is not Open Kernel Module (${NVIDIA_VER_LINE:-version unknown}; dmabuf export unavailable)"
fi

echo ""

# --- 1.6 GPU Memory Mapping: Using nvidia-peermem ------------------------------
#
# A prebuilt driver package is compiled without the MLNX_OFED RDMA symbols, so
# nvidia-peermem loads but never registers as a peer memory client and GPU
# memory MR registration fails. Check the built module, not just the package.

echo "=== 1.6 GPU Memory Mapping: nvidia-peermem ==="
dpkg -l 2>/dev/null | grep -E '^ii[[:space:]]+nvidia-(driver|dkms)(-[0-9]+)?(-open)?[[:space:]]' \
  | awk '{print "  [INFO] " $2 " " $3}'

if dkms status 2>/dev/null | grep -qi nvidia; then
  pass "nvidia module is DKMS-managed"
else
  fail "no DKMS-managed nvidia module (prebuilt driver lacks RDMA symbols)"
fi

PEERMEM_KO=$(find "/lib/modules/$(uname -r)" -name "nvidia-peermem.ko*" 2>/dev/null | head -1)
if [[ -z "${PEERMEM_KO}" ]]; then
  fail "nvidia-peermem.ko not found for kernel $(uname -r)"
else
  CHECK_KO="${PEERMEM_KO}"
  TMP_KO=""
  if [[ "${PEERMEM_KO}" == *.zst ]]; then
    TMP_KO=$(mktemp -t nvidia-peermem.XXXXXX.ko)
    if zstd -d -f -q -o "${TMP_KO}" "${PEERMEM_KO}" 2>/dev/null; then
      CHECK_KO="${TMP_KO}"
    fi
  fi
  if nm -u "${CHECK_KO}" 2>/dev/null | grep -q peer_memory_client; then
    pass "nvidia-peermem built with RDMA support (peer_memory_client)"
  else
    fail "nvidia-peermem lacks peer_memory_client; rebuild via DKMS (ENVIRONMENT_SETUP.md 1.6)"
  fi
  [[ -n "${TMP_KO}" ]] && rm -f "${TMP_KO}"
fi

if lsmod 2>/dev/null | grep -q '^nvidia_peermem'; then
  pass "nvidia_peermem loaded"
else
  fail "nvidia_peermem not loaded (sudo modprobe nvidia-peermem)"
fi

echo ""

# --- 1.6 Post-installation Actions ---------------------------------------------
#
# Official post-driver-install checklist (NVIDIA Driver Installation Guide —
# Post-installation Actions). Applies after any NVIDIA driver install/switch,
# regardless of which GPU memory mapping path (dmabuf/nvidia-peermem) is used.
# The Persistence Daemon, not `nvidia-smi -pm 1` (checked above under 1.6 CUDA
# dependency as the provisional measure), is the durable mechanism: if the
# daemon is active, that manual step is unnecessary.

echo "=== 1.6 Post-installation Actions ==="
if systemctl is-active --quiet nvidia-persistenced 2>/dev/null \
   && systemctl is-enabled --quiet nvidia-persistenced 2>/dev/null; then
  pass "nvidia-persistenced enabled and active"
else
  fail "nvidia-persistenced not enabled/active (systemctl status nvidia-persistenced)"
fi

info "driver version: $(head -1 /proc/driver/nvidia/version 2>/dev/null || echo 'unknown')"

echo ""

# --- 1.6 GPU BAR1 Size ----------------------------------------------------------
#
# Some GPUs (e.g. RTX series) ship with a small BAR1 (~256 MiB). If BAR1 is too
# small, DOCA cannot mmap the GPU memory buffer to the NIC and the application
# exits with an error. There is no official hard minimum, so treat a small
# value as a warning (not a hard failure) and report the actual size.

echo "=== 1.6 GPU BAR1 Size ==="
BAR1_LIST=$(nvidia-smi -q 2>/dev/null | grep -A3 "BAR1 Memory Usage" | grep -oP 'Total\s*:\s*\K[0-9]+')
if [[ -z "${BAR1_LIST}" ]]; then
  fail "could not determine BAR1 size (nvidia-smi -q | grep -i bar)"
else
  MIN_BAR1=$(echo "${BAR1_LIST}" | sort -n | head -1)
  if [[ "${MIN_BAR1}" -lt 1024 ]]; then
    warn "smallest BAR1 total ${MIN_BAR1} MiB (< 1024 MiB; DOCA mmap of GPU memory to the NIC may fail — enable Resizable BAR in BIOS)"
  else
    pass "BAR1 total >= ${MIN_BAR1} MiB on all GPUs"
  fi
fi

echo ""

# --- 1.6 Running without Root Privileges -----------------------------------------
#
# Optional: only relevant for running Verbs/RDMA/DMA samples without root.
# Ethernet DOCA Flow samples require root regardless, so absence is not a
# failure by itself.

echo "=== 1.6 Running without Root Privileges ==="
REG_LINE=$(grep RegistryDwords /proc/driver/nvidia/params 2>/dev/null)
if echo "${REG_LINE}" | grep -q "PeerMappingOverride=1"; then
  pass "PeerMappingOverride=1 set (Verbs/RDMA/DMA samples can run without root)"
else
  info "PeerMappingOverride not set (optional; needed only to run Verbs/RDMA/DMA samples without root — Ethernet/DOCA Flow samples require root regardless)"
fi

echo ""

# --- Summary ------------------------------------------------------------------

echo "========================================"
echo "  $(hostname): ${PASS_COUNT} passed, ${FAIL_COUNT} failed, ${WARN_COUNT} warned"
echo "========================================"

if [[ "${FAIL_COUNT}" -gt 0 ]]; then
  exit 1
fi
