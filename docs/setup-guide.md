# Environment Setup

This guide explains how to set up the environments used by gpu-direct-comm. Choose the section that matches your use case.

> 日本語版はこちら: [setup-guide.ja.md](./setup-guide.ja.md)

## 1. Local Cluster

For running the controller and its dependencies against a k3d cluster. This is the standard development workflow.

### Required tools

- Go 1.25+
- Docker
- [k3d](https://k3d.io/)
- kubectl (version matching k3d's Kubernetes version)
- [Numaflow](https://numaflow.numaproj.io/)
- [DRANET](https://github.com/kubernetes-sigs/dranet)
- [cert-manager](https://cert-manager.io/)
- [whereabouts](https://github.com/k8snetworkingplumbingwg/whereabouts)

### Prerequisite environment (cluster + Numaflow)

#### 1. k3d cluster creation

Create a cluster using the config file at the repository root:

```bash
k3d cluster create --config k3d-config.yaml
```

This command does the following:

1. Creates a k3d cluster (k3s running inside Docker containers)
2. Registers the cluster as `k3d-numaflow-cluster` in `~/.kube/config`
3. Sets `current-context` to `k3d-numaflow-cluster` automatically (configured by `switchCurrentContext: true` in `k3d-config.yaml`)

After this, all `kubectl` commands target the k3d cluster via `127.0.0.1:6443` (the k3s API server port forwarded from the Docker container to the host).

You can verify the context is set correctly:

```bash
kubectl config current-context
# Expected: k3d-numaflow-cluster
```

#### 2. Numaflow installation

Install Numaflow from its published release manifest — no need to build it yourself unless you are developing Numaflow itself (see the optional path below):

```bash
kubectl create namespace numaflow-system
kubectl apply -n numaflow-system -f https://raw.githubusercontent.com/numaproj/numaflow/stable/config/install.yaml
```

Because the previous step set `current-context` in `~/.kube/config` to `k3d-numaflow-cluster`, this `kubectl apply` targets the k3d cluster's API server — so Numaflow is deployed inside the k3d cluster, not on the host. Pin a specific released tag instead of `stable` (e.g. `.../numaflow/v1.7.1/config/install.yaml`) if you need to match a specific version.

After installation, verify that all Numaflow components are running:

```bash
kubectl get pods -n numaflow-system
# Expected: numaflow-controller, numaflow-server, numaflow-dex-server
```

> `config/install.yaml` does not include `numaflow-webhook` (Numaflow's own validating webhook for Pipeline/InterStepBufferService immutable-field checks) — it ships separately. It is optional: gpu-direct-comm's own webhooks do not depend on it. To install it as well:
>
> ```bash
> kubectl apply -n numaflow-system -f https://raw.githubusercontent.com/numaproj/numaflow/stable/config/validating-webhook-install.yaml
> kubectl get pods -n numaflow-system -l app.kubernetes.io/name=numaflow-webhook
> # Expected: numaflow-webhook — Running
> ```

> **Optional: build Numaflow from source.** Only needed if you are developing Numaflow itself (e.g. testing an unreleased change against this repository's webhook). Clone the [numaflow repository](https://github.com/numaproj/numaflow) and run:
>
> ```bash
> cd /path/to/numaflow
> IMAGE_NAMESPACE=<your-registry> VERSION=latest make start
> ```
>
> This builds the Numaflow container image and installs it into the cluster (again targeting `k3d-numaflow-cluster` via `current-context`), in place of the `kubectl apply` above. For the full Numaflow build environment setup (Go, Rust, protoc, etc.), see the [Numaflow Development](https://numaflow.numaproj.io/development/development/) documentation.

#### InterStepBufferService (ISBSvc) deployment

Numaflow Pipelines require an InterStepBufferService (JetStream) to be running and healthy before they can be created — Numaflow's own ValidatingWebhook rejects Pipeline creation if the ISBSvc is not yet ready. Deploy it once after Numaflow installation:

```bash
kubectl apply -f config/testdata/isbsvc.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Running isbsvc/default --timeout=120s
```

Verify the JetStream Pods are running:

```bash
kubectl get pods -l numaflow.numaproj.io/isbsvc-name=default
# Expected: isbsvc-default-js-0, isbsvc-default-js-1, isbsvc-default-js-2 — all Running, READY 3/3
```

### gpu-direct-comm environment setup

#### 3. DRANET installation

DRANET is the DRA (Dynamic Resource Allocation) driver that publishes NICs as `ResourceSlice` objects and attaches them to Pods.

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/dranet/refs/heads/main/install.yaml
kubectl -n kube-system rollout status ds/dranet --timeout=120s
```

Verify that DRANET is publishing devices:

```bash
kubectl get resourceslice
```

If this returns nothing, k3s's DRA feature gate may not be enabled. k3s v1.34.x enables `DynamicResourceAllocation` as beta by default, but if you still see no output, add the feature gate explicitly to `k3d-config.yaml` and recreate the cluster:

```yaml
options:
  k3s:
    extraArgs:
      - arg: "--feature-gates=DynamicResourceAllocation=true"
        nodeFilters: ["server:*", "agent:*"]
```

#### 4. DeviceClass creation

A `DeviceClass` tells Kubernetes which DRANET-published devices are eligible for allocation. `NumaNetwork.spec.refDeviceClass.name` must reference this object. The local cluster uses a DeviceClass that filters to `dummy` type interfaces only:

```bash
kubectl apply -f config/testdata/e2e_deviceclass_dranet_local.yaml
```

Verify that the DeviceClass exists:

```bash
kubectl get deviceclass dranet-e2e-local
# Expected: the DeviceClass with AGE
```

#### 5. whereabouts installation

whereabouts is the CNI IPAM plugin that `webhook-whereabouts-numanetwork` execs to allocate IPs from `NumaNetwork.spec.refResourceClaimDranet.ipRange`. Its DaemonSet also writes a per-node flat config file (`/etc/cni/net.d/whereabouts.d/whereabouts.conf`, including a kubeconfig for talking to the IPPool CRDs) that `webhook-whereabouts-numanetwork` depends on — install it before deploying the webhook.

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/dranet/main/tests/manifests/whereabouts_upstream.yaml
kubectl -n kube-system rollout status ds/whereabouts --timeout=120s
```

Verify that whereabouts wrote its config on each node. The DaemonSet mounts the host's `/etc/cni/net.d` at `/host/etc/cni/net.d` inside its own container, so `kubectl exec` into it reads the host file directly — this works identically on the Local and Bare-metal clusters, with no dependency on SSH keys or sudo access to the nodes:

```bash
kubectl -n kube-system exec ds/whereabouts -- cat /host/etc/cni/net.d/whereabouts.d/whereabouts.conf
# Expected: JSON with "kubeconfig" field pointing to a valid path
```

#### 6. cert-manager installation

The gpu-direct-comm controller manager's webhook (`internal/webhook/v1alpha1`) requires TLS certificates managed by cert-manager (`config/default/kustomization.yaml` includes `../certmanager`).

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available --timeout=120s deployment --all
```

Verify that all cert-manager Pods are running:

```bash
kubectl get pods -n cert-manager
# Expected: cert-manager, cert-manager-cainjector, cert-manager-webhook — all Running
```

#### 7. gpu-direct-comm installation

The following three sub-steps install components built from this repository's source code.

##### 7-1. gpu-direct-comm CRD installation

```bash
make install
```

This runs `kustomize build config/crd | kubectl apply -f -`, registering the `NumaNetwork` CRD. This CRD is defined by gpu-direct-comm (not included in Numaflow's upstream `config/install.yaml`).

Verify that the CRD is registered:

```bash
kubectl get crd numanetworks.numaflow.numaproj.io
# Expected: the CRD with CREATED AT timestamp
```

##### 7-2. gpu-direct-comm controller manager deployment

```bash
make docker-build IMG=controller:latest
k3d image import controller:latest -c numaflow-cluster
make deploy IMG=controller:latest
kubectl -n gpu-direct-comm-system rollout status deployment/gpu-direct-comm-controller-manager --timeout=120s
```

Verify that the controller manager Pod is ready:

```bash
kubectl get pods -n gpu-direct-comm-system
# Expected: gpu-direct-comm-controller-manager-... — Running, READY 1/1
```

##### 7-3. webhook-whereabouts-numanetwork build and deployment

`webhook-whereabouts-numanetwork` is the custom dranet BYODP (Bring Your Own DRANET Provider) webhook implemented in this repository (`cmd/webhook-whereabouts-numanetwork`, `internal/ipam`). It resolves a `NumaNetwork`'s `ipRange` and execs `whereabouts` to allocate an IP.

```bash
make docker-build-webhook-nn WEBHOOK_NN_IMG=webhook-whereabouts-numanetwork:latest
k3d image import webhook-whereabouts-numanetwork:latest -c numaflow-cluster
kubectl apply -k config/webhook-whereabouts-numanetwork/
kubectl -n kube-system rollout status ds/webhook-whereabouts-numanetwork --timeout=60s
```

Verify that the webhook Pods are ready. `READY 1/1` means the readinessProbe
already confirmed `/health` is responding — no separate health check is needed:

```bash
kubectl -n kube-system get pods -l app.kubernetes.io/name=webhook-whereabouts-numanetwork
# Expected: one Pod per node — all Running, READY 1/1
```

### Verify

Run all checks at once to confirm the environment is fully operational:

```bash
# k3d cluster context
kubectl config current-context
# Expected: k3d-numaflow-cluster

# Numaflow components
kubectl get pods -n numaflow-system
# Expected: numaflow-controller, numaflow-server, numaflow-dex-server — all Running (plus numaflow-webhook if you installed the optional validating webhook)

# DRANET devices
kubectl get resourceslice
# Expected: one or more ResourceSlice objects

# DeviceClass
kubectl get deviceclass dranet-e2e-local
# Expected: the DeviceClass with AGE

# whereabouts config on a node
kubectl -n kube-system exec ds/whereabouts -- cat /host/etc/cni/net.d/whereabouts.d/whereabouts.conf
# Expected: JSON with "kubeconfig" field

# cert-manager
kubectl get pods -n cert-manager
# Expected: cert-manager, cert-manager-cainjector, cert-manager-webhook — all Running

# gpu-direct-comm CRD
kubectl get crd numanetworks.numaflow.numaproj.io
# Expected: the CRD with CREATED AT timestamp

# gpu-direct-comm controller manager
kubectl get pods -n gpu-direct-comm-system
# Expected: gpu-direct-comm-controller-manager-... — Running, READY 1/1

# DaemonSets (DRANET + whereabouts + webhook-whereabouts-numanetwork)
kubectl -n kube-system get ds whereabouts dranet webhook-whereabouts-numanetwork
# Expected: all DaemonSets READY on every node
```

---

## 2. Bare-metal Cluster

For running the controller on a multi-node bare-metal cluster with real NVIDIA GPU and SR-IOV VF hardware.

### Hardware prerequisites

Each worker node must have the following hardware installed.

#### GPU

- NVIDIA GPU (DRA driver compatible — set up via the `dra-driver-nvidia-gpu` role in `numaflow-dra-ansible`)

#### d-plane NIC

Each worker node must have a NIC connected to the data plane (d-plane) that satisfies the following requirements.

| Requirement | Description |
|-------------|-------------|
| RDMA capable | The NIC must support RDMA verbs at the hardware level. GPU Direct RDMA performs device-to-device communication and cannot function with a non-RDMA NIC |
| SR-IOV capable | The NIC must support creating SR-IOV Virtual Functions (VFs). DRANET publishes VFs as `ResourceSlice` objects and assigns them to Pods via DRA |
| GPUDirect RDMA capable (when using GPU Direct RDMA) | The NIC must be able to directly access GPU memory via peer memory. For best performance, the GPU and NIC should be placed under the same PCIe switch (PIX topology) |

**NICs with verified track record:**

| Vendor | NIC | Driver | RDMA protocol | Notes |
|--------|-----|--------|---------------|-------|
| NVIDIA/Mellanox | ConnectX-6 or later | `mlx5_core` (OFED) | RoCE v2 (Ethernet) / Native IB RDMA (InfiniBand) | Verified for GPUDirect RDMA. VPI cards may require port mode switching (see [Step 2: Determine and configure the port mode](#step-2-determine-and-configure-the-port-mode)) |
| Intel | E810 | `ice` + `irdma` | RoCE v2 | Limited GPUDirect RDMA support |

> Port mode must match the switch type (Ethernet switch → Ethernet mode / RoCE v2, InfiniBand switch → InfiniBand mode / Native IB RDMA). See [Step 2: Determine and configure the port mode](#step-2-determine-and-configure-the-port-mode) for details.

### Required tools

- Ansible control node (`ansible-core` >= 2.16) — used to drive [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible)
- SSH access to every managed node
- kubectl (version matching the cluster's Kubernetes version — see `vars-stg.yml` in numaflow-dra-ansible)
- Docker, for building the controller and webhook images
- A container registry reachable from every cluster node (bare-metal nodes cannot use `k3d image import`)
- [DRANET](https://github.com/kubernetes-sigs/dranet), [cert-manager](https://cert-manager.io/), [whereabouts](https://github.com/k8snetworkingplumbingwg/whereabouts) — installed in the gpu-direct-comm environment setup step below, not by the ansible playbook

### Prerequisite environment (cluster, GPU, DRA, Numaflow)

Cluster provisioning, the NVIDIA GPU driver/toolkit, GPU DRA driver enablement, and Numaflow installation are delegated to [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) (`~/project/numaflow-dra-ansible` in this workspace). Follow that repository's own README for inventory setup (`inventory/stg.yml`, copied from `inventory/inventory.yml.template`), then run the root playbook from within it:

```bash
cd ~/project/numaflow-dra-ansible
ansible-playbook -i inventory/stg.yml -e @vars-stg.yml site-stg-dci-poc.yml
```

This playbook installs, in order (see `site-stg-dci-poc.yml`):

1. Ubuntu prerequisites
2. Kubernetes cluster via kubeadm + Calico CNI (`playbooks/kubernetes-cluster.yml`)
3. NVIDIA GPU driver + container toolkit (`playbooks/nvidia-gpu-support.yml`)
4. DRA feature gate + NVIDIA GPU DRA driver (`playbooks/dra-driver-nvidia-gpu.yml`)
5. Numaflow (`playbooks/numaflow.yml`)
6. Prometheus monitoring for Numaflow (`playbooks/monitor.yml`)

> This playbook does **not** install DRANET, whereabouts, or any gpu-direct-comm component — those are set up in the next section, the same way as on the Local Cluster.

Verify each layer before continuing:

```bash
# Confirm the active context targets the bare-metal API server, not a local k3d cluster
kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'
# Expected: a real node IP (e.g. https://<control-plane-node-ip>:6443) — NOT https://127.0.0.1:6443
# (127.0.0.1:6443 would mean kubectl is pointed at a local k3d cluster instead; see the Local Cluster section)

# Kubernetes cluster
kubectl get nodes
# Expected: all nodes Ready

# NVIDIA GPU DRA driver
kubectl get pods -n nvidia-dra-driver-gpu
# Expected: nvidia-dra-driver-gpu-controller, nvidia-dra-driver-gpu-kubeletplugin — all Running

# Numaflow components
kubectl get pods -n numaflow-system
# Expected: numaflow-controller, numaflow-server, numaflow-dex-server — all Running

# Prometheus monitoring
kubectl get pods -n monitoring
# Expected: prometheus-k8s, prometheus-operator and related Pods — all Running
```

> As with the Local Cluster's default YAML install, `numaflow-webhook` is **not** expected here either. `numaflow-dra-ansible`'s `numaflow_install` role applies Numaflow's `config/install.yaml` — the same base manifest the Local Cluster uses by default — which does not include it (see the note under [Local Cluster > Numaflow installation](#numaflow-installation)). It is optional — gpu-direct-comm's own webhooks do not depend on it — but if you want it verified on bare-metal too, install it manually against the same Numaflow version the ansible playbook installed (see `numaflow_install.numaflow_version` in `vars-stg.yml`):
>
> ```bash
> kubectl apply -n numaflow-system -f https://raw.githubusercontent.com/numaproj/numaflow/<numaflow_version>/config/validating-webhook-install.yaml
> kubectl get pods -n numaflow-system -l app.kubernetes.io/name=numaflow-webhook
> # Expected: numaflow-webhook — Running
> ```

### gpu-direct-comm environment setup

DRANET, the `dranet` DeviceClass, whereabouts, cert-manager, and gpu-direct-comm's own components (CRD, controller manager, `webhook-whereabouts-numanetwork`) are **not** part of `numaflow-dra-ansible`. Install them the same way as the [Local Cluster](#1-local-cluster) flow, with the substitutions noted below.

The install order reflects a dependency chain: each component relies on the one before it.

| Step | Component | Role | Depends on |
|------|-----------|------|------------|
| 1 | SR-IOV VF | Creates virtual NIC interfaces on each node for DRANET to discover | Host NIC hardware + OFED driver |
| 2 | DRANET | Scans node interfaces and publishes them as `ResourceSlice` objects via DRA | VFs existing on the node |
| 3 | DeviceClass | CEL selector that tells Kubernetes which `ResourceSlice` devices are allocatable | DRANET publishing the VFs |
| 4 | whereabouts | CNI IPAM plugin that allocates IPs from `ipRange`; writes per-node config that the webhook reads | — |
| 5 | cert-manager | Issues TLS certificates for the controller manager's admission webhook | — |
| 6 | gpu-direct-comm | Controller + mutating webhook + BYODP webhook that ties VF allocation to IP assignment | All of the above |

> Steps 4 and 5 have no dependency on steps 1–3 and can be installed in any order relative to them, but must complete before step 6.

#### 1. SR-IOV VF preparation

SR-IOV Virtual Functions (VFs) must exist on each worker node's RDMA-capable NIC **before** DRANET is installed. DRANET's DaemonSet scans node interfaces on startup — if VFs do not exist yet, they will not appear as `ResourceSlice` entries.

> VF creation only needs to be done once per node (it persists across reboots if made persistent via systemd — see below). If your environment already has VFs from a previous setup, skip to [Verify VFs are visible](#verify-vfs-are-visible).

##### Step 1: Verify the RDMA-capable NIC is recognized

SSH into a worker node and confirm that the Mellanox/NVIDIA NIC is visible on the PCI bus:

```bash
lspci | grep -i mellanox
# Example output:
#   6a:00.0 Infiniband controller: Mellanox Technologies MT28908 Family [ConnectX-6]
#   6a:00.1 Infiniband controller: Mellanox Technologies MT28908 Family [ConnectX-6]
```

If no output appears, the NIC may not be physically seated or the driver is not loaded.

##### Step 2: Check port mode

ConnectX VPI cards ship with ports defaulting to **InfiniBand mode**. The required mode depends on the switch the NIC is connected to:

| Connected switch type | Required port mode | RDMA protocol |
|-----------------------|-------------------|---------------|
| InfiniBand switch     | InfiniBand (default) | Native IB RDMA |
| Ethernet switch       | Ethernet          | RoCE v2 |

```bash
# Install mstflint if not already present (provides mstconfig)
sudo apt install -y mstflint

# Check the port mode of each physical link
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')
sudo mstconfig -d 0000:${PCI_ADDR} query | grep LINK_TYPE
# Output example:
#   LINK_TYPE_P1                        IB(1)
#   LINK_TYPE_P2                        IB(1)
```

Verify the port mode matches your expectation. If it does not, it will be changed in Step 5.

##### Step 3: Check link state

**Identify which physical port has a cable connected.** Dual-port cards have two independent PFs — only the port with link will be used for VF creation.

The verification method differs depending on the port mode confirmed in the previous step.

| Port mode  | Typical interface name | Where to look |
|------------|----------------------|---------------|
| Ethernet   | `enp<bus>s<slot>f<func>np<port>` | `/sys/class/net/` |
| InfiniBand | `ib0`, `ib1` (IPoIB) | `/sys/class/net/` |
| InfiniBand (RDMA device) | `mlx5_0`, `mlx5_1` | `/sys/class/infiniband/` |

For **Ethernet mode** (already showing "Ethernet controller" in `lspci`), check link state via sysfs:

```bash
for PCI_ADDR in $(lspci | grep -i mellanox | awk '{print $1}'); do
  IFACE=$(ls -l /sys/class/net/ | grep "${PCI_ADDR}" | awk '{print $9}')
  echo "${IFACE}: $(cat /sys/class/net/${IFACE}/operstate)"
done
# Output example:
#   enp101s0f0np0: up
#   enp101s0f1np1: down
```

For **InfiniBand mode**, check via the RDMA device:

```bash
for PCI_ADDR in $(ls /sys/class/infiniband/); do
  STATE=$(cat /sys/class/infiniband/${PCI_ADDR}/ports/1/state)
  echo "${PCI_ADDR}: ${STATE}"
done
# Output example:
#   mlx5_0: 4: ACTIVE     ← cable connected
#   mlx5_1: 1: DOWN       ← no cable
```

Note the interface name of the port with link — it will be used in VF status check and VF creation later.

##### Step 4: Check VF creation status

```bash
IFACE="<your-pf-name>"   # e.g. enp4s0f0np0 (Ethernet) or ib0 (InfiniBand)

# Verify SR-IOV is enabled in firmware
cat /sys/class/net/${IFACE}/device/sriov_totalvfs
# Expected: 8 (the NUM_OF_VFS value set in Step 5)
```

If `sriov_totalvfs` does not exist, SR-IOV is not enabled at the firmware level. Enable it in the next step.

##### Step 5: Change port mode and enable SR-IOV

**Apply firmware settings.** Port mode change and SR-IOV enablement are both firmware-level settings that require a reboot, so apply them together.

Apply with a single `mstconfig` command against the address of the port with link. Run one of the following:

```bash
# head -1 or -2
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')

# Change port mode
# Set ETH for Ethernet or IB for InfiniBand depending on your lspci output
sudo mstconfig -d 0000:${PCI_ADDR} set  LINK_TYPE_P1=ETH LINK_TYPE_P2=ETH

# Enable SR-IOV
# If SR-IOV was not enabled in Step 4, run this instead
# Enable SR-IOV and set the maximum number of VFs (8 is more than enough for E2E testing)
sudo mstconfig -d 0000:${PCI_ADDR} set SRIOV_EN=1 NUM_OF_VFS=8 LINK_TYPE_P1=ETH LINK_TYPE_P2=ETH

# A reboot is required for firmware changes to take effect
sudo reboot
```

After reboot, verify:
- `lspci | grep -i mellanox` should show **"Ethernet controller"** (if you switched mode)
- `cat /sys/class/net/<pf-name>/device/sriov_totalvfs` should return `8` (the NUM_OF_VFS value set in Step 5)

##### Step 6: Create VFs

```bash
# For Ethernet
## head -1 or -2
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')
IFACE=$(ls -l /sys/class/net/ | grep "$PCI_ADDR" | awk '{print $9}')

# For InfiniBand (head -1 or -2 to select the ACTIVE device)
RDMA_DEV=$(ls /sys/class/infiniband/ | head -1)
IFACE=$(ls /sys/class/infiniband/${RDMA_DEV}/device/net/ | head -1)

# Verify the number of VFs that can be created
cat /sys/class/net/${IFACE}/device/sriov_totalvfs

# Create VFs (2 is sufficient for E2E testing — one per vertex Pod)
sudo sh -c "echo 2 > /sys/class/net/${IFACE}/device/sriov_numvfs"

# Confirm VF network interfaces exist
ip link show | grep -E 'vf '

# Confirm VF PCI devices are visible
lspci | grep -i 'virtual function'
# Expected: one line per VF (e.g. "04:00.2 ... Virtual Function")
```

Repeat on every worker node that will run gpu-direct workloads.

##### Step 7: Make VF creation persistent across reboots

Without persistence, VFs disappear on reboot. Create a systemd oneshot service on each worker node:

```bash
cat <<EOF | sudo tee /etc/systemd/system/sriov-vf@.service
[Unit]
Description=Create SR-IOV VFs on %i
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/bin/bash -c 'echo 2 > /sys/class/net/%i/device/sriov_numvfs'
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

# Identify the PF name (after VF creation, VFs also match the same PCI address — exclude vN suffixes)
# head -1 or -2
PCI_ADDR=$(lspci | grep -i mellanox | head -1 | awk '{print $1}')
PF_NAME=$(ls -l /sys/class/net/ | grep "$PCI_ADDR" | grep -v 'v[0-9]' | awk '{print $9}')

sudo systemctl enable --now sriov-vf@${PF_NAME}.service
```

#### 2. DRANET installation

DRANET scans each node's network interfaces (including the VFs created above) and publishes them as `ResourceSlice` objects via Kubernetes DRA. Without DRANET, the cluster has no way to allocate VFs to Pods.

Identical to [Local Cluster > 3. DRANET installation](#3-dranet-installation). The `DynamicResourceAllocation` feature gate is enabled by the ansible playbook's `feature_gates_dra_master` role (see the previous step), so if `kubectl get resourceslice` returns nothing after installing DRANET, check that role's result rather than editing `k3d-config.yaml` — that file only applies to the Local Cluster. After installation, confirm that DRANET detected the SR-IOV VFs:

```bash
kubectl get resourceslice -o yaml | grep -A5 'ifName'
# Expected: entries for your VF interface names with dra.net/type: sriov (or similar)
```

#### 3. DeviceClass creation

A `DeviceClass` defines a CEL selector that filters which DRANET-published `ResourceSlice` devices are eligible for allocation. It must be created **after** DRANET, because the selector references attributes (e.g. `dra.net/sriov`) that only exist once DRANET has published the devices.

The bare-metal DeviceClass filters to SR-IOV VFs only. DRANET sets `dra.net/isSriovVf: true` exclusively on VFs; PFs, non-SR-IOV physical NICs, and software interfaces lack this attribute entirely:

```bash
kubectl apply -f config/testdata/e2e_deviceclass_dranet_baremetal.yaml
```

Verify that the DeviceClass exists:

```bash
kubectl get deviceclass dranet-e2e-baremetal
# Expected: the DeviceClass with AGE
```

#### 4. whereabouts installation

Identical to [Local Cluster > 5. whereabouts installation](#5-whereabouts-installation).

#### 5. cert-manager installation

Identical to [Local Cluster > 6. cert-manager installation](#6-cert-manager-installation).

#### 6. gpu-direct-comm installation

The controller reconciles `NumaNetwork` into `ResourceClaimTemplate` (using the DeviceClass), the mutating webhook injects claims into Pipeline Pods, and `webhook-whereabouts-numanetwork` calls whereabouts to assign IPs — so all upstream components (steps 1–5) must be ready first.

Bare-metal nodes cannot use `k3d image import`, so images must be pushed to a registry every node can pull from. The sub-steps below replace [Local Cluster > 7. gpu-direct-comm installation](#7-gpu-direct-comm-installation).

##### 6-1. gpu-direct-comm CRD installation

Identical to [Local Cluster > 7-1. gpu-direct-comm CRD installation](#7-1-gpu-direct-comm-crd-installation):

```bash
make install
kubectl get crd numanetworks.numaflow.numaproj.io
# Expected: the CRD with CREATED AT timestamp
```

##### 6-2. Prepare an image registry

Provision (or reuse) a container registry that both your build host and every cluster node can reach — for example an internal Harbor instance. The steps below use `<registry>/<project>` as a placeholder for its address and project/repository path; substitute your own.

##### 6-3. gpu-direct-comm controller manager deployment

```bash
make docker-build IMG=<registry>/<project>/controller:<tag>
make docker-push IMG=<registry>/<project>/controller:<tag>
make deploy IMG=<registry>/<project>/controller:<tag>
kubectl -n gpu-direct-comm-system rollout status deployment/gpu-direct-comm-controller-manager --timeout=120s
```

`make deploy` runs `kustomize edit set image controller=<IMG>` under the hood, rewriting `config/manager/kustomization.yaml` in place — this is expected and does not need to be committed.

Verify:

```bash
kubectl get pods -n gpu-direct-comm-system
# Expected: gpu-direct-comm-controller-manager-... — Running, READY 1/1
```

##### 6-4. webhook-whereabouts-numanetwork build and deployment

Unlike the controller manager, there is no `docker-push`/`deploy` Make target for this image, and `config/webhook-whereabouts-numanetwork/kustomization.yaml` does not yet have an `images:` transformer — `kustomize edit set image` adds one the first time you run it:

```bash
make docker-build-webhook-nn WEBHOOK_NN_IMG=<registry>/<project>/webhook-whereabouts-numanetwork:<tag>
docker push <registry>/<project>/webhook-whereabouts-numanetwork:<tag>
make kustomize
(cd config/webhook-whereabouts-numanetwork && ../../bin/kustomize edit set image webhook-whereabouts-numanetwork=<registry>/<project>/webhook-whereabouts-numanetwork:<tag>)
kubectl apply -k config/webhook-whereabouts-numanetwork/
kubectl -n kube-system rollout status ds/webhook-whereabouts-numanetwork --timeout=60s
```

As with `config/manager/kustomization.yaml`, this rewrites `config/webhook-whereabouts-numanetwork/kustomization.yaml` in place — expected, no need to commit.

Verify:

```bash
kubectl -n kube-system get pods -l app.kubernetes.io/name=webhook-whereabouts-numanetwork
# Expected: one Pod per node the DaemonSet can schedule onto — all Running, READY 1/1
```

### Verify

Run the same checklist as [Local Cluster > Verify](#verify), with one substitution: skip the `kubectl config current-context` check (bare-metal clusters are not created by k3d). The whereabouts config check (`kubectl -n kube-system exec ds/whereabouts -- cat ...`) is identical — no SSH access to the nodes is needed for it.

---

## Notes

- The k3d cluster configuration is in `k3d-config.yaml` at the repository root (Local Cluster only).
- k3d automatically updates `~/.kube/config` and switches the current context to the new cluster (`switchCurrentContext: true` in the config).
- [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) automates cluster/GPU/DRA/Numaflow provisioning for the Bare-metal Cluster. It does not cover DRANET, whereabouts, or any gpu-direct-comm component — those are always set up manually via the steps in this guide, on both Local and Bare-metal clusters.
