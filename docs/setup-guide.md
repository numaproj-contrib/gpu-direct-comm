# Development Environment Setup

This guide explains how to set up a development environment for gpu-direct-comm. Choose the section that matches your use case.

## 1. Unit Test Development

For writing and running unit tests only. No Kubernetes cluster is required.

### Required tools

- Go 1.25+

### Setup steps

<!-- TODO: Fill in -->

### Verify

```bash
make test
```

---

## 2. Local Cluster Development

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

<!-- TODO: Fill in remaining steps. Cover:
  - cert-manager installation
  - DRANET installation and DeviceClass configuration
  - whereabouts installation
  - CRD installation (make install)
  - webhook-whereabouts-numanetwork deployment
-->

### Verify

```bash
kubectl get pods -n numaflow-system
kubectl get deviceclass
make run
```

---

## 3. E2E Test Execution

For running the end-to-end test suite.

### Required tools

All tools from [Local Cluster Development](#2-local-cluster-development).

### Setup steps

<!-- TODO: Fill in. Cover:
  - Any additional setup beyond Local Cluster Development
  - Test cluster lifecycle (creation, teardown)
-->

### Run

```bash
make test-e2e
```

---

## Notes

- The k3d cluster configuration is in `k3d-config.yaml` at the repository root.
- k3d automatically updates `~/.kube/config` and switches the current context to the new cluster (`switchCurrentContext: true` in the config).
- The [numaflow-dra-ansible](https://github.com/compsysg/numaflow-dra-ansible) playbook can automate much of this setup. See its README for details.
