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

### Setup steps

#### k3d cluster creation

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

#### Numaflow installation

Numaflow is installed by building and deploying from the [numaflow repository](https://github.com/numaproj/numaflow). Clone the repository and run:

```bash
cd /path/to/numaflow
IMAGE_NAMESPACE=<your-registry> VERSION=latest make start
```

This command builds the Numaflow container image and installs it into the cluster. Because the previous step set `current-context` in `~/.kube/config` to `k3d-numaflow-cluster`, `make start` internally runs `kubectl apply` against the k3d cluster's API server — so Numaflow is deployed inside the k3d cluster, not on the host.

After installation, verify that all Numaflow components are running:

```bash
kubectl get pods -n numaflow-system
# Expected: numaflow-controller, numaflow-server, numaflow-dex-server, numaflow-webhook
```

For the full Numaflow build environment setup (Go, Rust, protoc, etc.), see the [Numaflow Development](https://numaflow.numaproj.io/development/development/) documentation.

#### cert-manager installation

The controller's webhook (`internal/webhook/v1alpha1`) requires TLS certificates managed by cert-manager (`config/default/kustomization.yaml` includes `../certmanager`).

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.20.2/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available --timeout=120s deployment --all
```

Verify that all cert-manager Pods are running:

```bash
kubectl get pods -n cert-manager
# Expected: cert-manager, cert-manager-cainjector, cert-manager-webhook — all Running
```

#### CRD installation

```bash
make install
```

This runs `kustomize build config/crd | kubectl apply -f -`, registering the `NumaNetwork` CRD (and any Numaflow CRDs it depends on — see the Numaflow installation step above).

Verify that the CRD is registered:

```bash
kubectl get crd numanetworks.numaflow.numaproj.io
# Expected: the CRD with CREATED AT timestamp
```

#### DRANET installation

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

#### DeviceClass creation

A `DeviceClass` tells Kubernetes which DRANET-published devices are eligible for allocation. `NumaNetwork.spec.refDeviceClass.name` must reference this object:

```bash
kubectl apply -f config/samples/deviceclass_dranet.yaml
```

Verify that the DeviceClass exists:

```bash
kubectl get deviceclass dranet
# Expected: the DeviceClass with AGE
```

> This basic filter matches every DRANET-published device, including the node's own bridge interfaces (e.g. `cni0`). For E2E testing with a dummy interface, a narrower selector is used — see [CONTRIBUTING.md](../CONTRIBUTING.md#local-cluster).

#### whereabouts installation

whereabouts is the CNI IPAM plugin that `webhook-whereabouts-numanetwork` execs to allocate IPs from `NumaNetwork.spec.refResourceClaimDranet.ipRange`. Its DaemonSet also writes a per-node flat config file (`/etc/cni/net.d/whereabouts.d/whereabouts.conf`, including a kubeconfig for talking to the IPPool CRDs) that `webhook-whereabouts-numanetwork` depends on — install it before deploying the webhook.

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/dranet/main/tests/manifests/whereabouts_upstream.yaml
kubectl -n kube-system rollout status ds/whereabouts --timeout=120s
```

Verify that whereabouts wrote its config on each node:

```bash
docker exec k3d-numaflow-cluster-server-0 cat /etc/cni/net.d/whereabouts.d/whereabouts.conf
# Expected: JSON with "kubeconfig" field pointing to a valid path
```

#### Deploy the controller manager

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

#### Build and deploy webhook-whereabouts-numanetwork

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
# Expected: numaflow-controller, numaflow-server, numaflow-dex-server, numaflow-webhook — all Running

# cert-manager
kubectl get pods -n cert-manager
# Expected: cert-manager, cert-manager-cainjector, cert-manager-webhook — all Running

# NumaNetwork CRD
kubectl get crd numanetworks.numaflow.numaproj.io
# Expected: the CRD with CREATED AT timestamp

# DRANET devices
kubectl get resourceslice
# Expected: one or more ResourceSlice objects

# DeviceClass
kubectl get deviceclass dranet
# Expected: the DeviceClass with AGE

# whereabouts config on a node
docker exec k3d-numaflow-cluster-server-0 cat /etc/cni/net.d/whereabouts.d/whereabouts.conf
# Expected: JSON with "kubeconfig" field

# Controller manager
kubectl get pods -n gpu-direct-comm-system
# Expected: gpu-direct-comm-controller-manager-... — Running, READY 1/1

# DaemonSets
kubectl -n kube-system get ds whereabouts dranet webhook-whereabouts-numanetwork
# Expected: all DaemonSets READY on every node
```

---

## 2. Bare-metal Cluster

For running the controller on a multi-node bare-metal cluster with real SR-IOV VF hardware.

> **TBD** — This section will be added once the bare-metal E2E workflow is validated.

---

## Notes

- The k3d cluster configuration is in `k3d-config.yaml` at the repository root.
- k3d automatically updates `~/.kube/config` and switches the current context to the new cluster (`switchCurrentContext: true` in the config).
- The [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) playbook can automate much of this setup. See its README for details.
