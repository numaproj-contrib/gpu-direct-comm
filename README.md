# gpu-direct-comm

<!-- Badges -->
![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-Apache_2.0-blue)
![Kubernetes](https://img.shields.io/badge/Kubernetes-1.32+-326CE5?logo=kubernetes&logoColor=white)

## Overview

**gpu-direct-comm** is a Kubernetes operator that extends [Numaflow](https://numaflow.numaproj.io/) with GPU Direct Communication capabilities. It manages secondary NICs for inter-vertex data transfer via RDMA. The operator uses Kubernetes Dynamic Resource Allocation (DRA) with [DRANET](https://github.com/kubernetes-sigs/dranet) to assign network interfaces and [whereabouts](https://github.com/k8snetworkingplumbingwg/whereabouts) for IP address management.

## Architecture

![Architecture Overview](docs/diagrams/architecture-overview.png)

> The editable source for the diagram above is `docs/diagrams/architecture-overview.drawio`.

### Components

| Component | Description |
|-----------|-------------|
| **NumaNetwork CRD** | Custom resource that declares a secondary network. Specifies `refDeviceClass` (DRA DeviceClass for NIC selection) and `refResourceClaimDranet` (IP range for IPAM via whereabouts). |
| **NumaNetwork Controller** | Reconciles NumaNetwork resources. Generates a `ResourceClaimTemplate` with DRANET opaque configuration. The profile name follows the `<namespace>/<name>` convention so the IPAM webhook can reverse-lookup the NumaNetwork at admission time. |
| **Pipeline ValidatingWebhook** | Validates that Pipeline annotations (`gpu-direct-comm.numaproj.io/numa-network-edges`) reference valid edges and existing NumaNetwork resources. Uses cert-manager for TLS. The Pipeline is decoded as `Unstructured` to avoid importing numaflow as a Go dependency. |
| **IPAM Webhook Provider** | A DaemonSet (`webhook-whereabouts-numanetwork`) that acts as the DRANET BYODP webhook provider. It parses the profile name to look up the NumaNetwork and fills in the whereabouts IP range at device admission time. *(Milestone 4 — In Progress)* |

## Prerequisites

- Go 1.25 or later
- Kubernetes 1.32 or later (with DRA feature gate enabled)
- [Numaflow](https://numaflow.numaproj.io/) installed in the cluster
- [DRANET](https://github.com/kubernetes-sigs/dranet) installed as the DRA driver
- [cert-manager](https://cert-manager.io/) for webhook TLS certificates
- [whereabouts](https://github.com/k8snetworkingplumbingwg/whereabouts) for IPAM (used via the BYODP webhook provider)

For step-by-step setup instructions, see [Environment Setup](docs/setup-guide.md).

## Quick Start

### 1. Install CRDs

```bash
make install
```

### 2. Deploy the controller

```bash
make deploy IMG=<your-registry>/gpu-direct-comm:<tag>
```

### 3. Create a NumaNetwork resource

```yaml
apiVersion: numaflow.numaproj.io/v1alpha1
kind: NumaNetwork
metadata:
  name: my-network
  namespace: default
spec:
  refDeviceClass:
    name: vf.nvidia.dra.net   # name of the DRA DeviceClass for the NIC type
  refResourceClaimDranet:
    ipRange: "192.168.10.0/24"
```

```bash
kubectl apply -f my-numanetwork.yaml
```

After reconciliation, the controller creates a `ResourceClaimTemplate` named `my-network-rct` in the same namespace.

### 4. Bind Pipeline edges to the NumaNetwork

Add the annotation to your Numaflow Pipeline to declare which edges should use GPU Direct Communication:

```yaml
apiVersion: numaflow.numaproj.io/v1alpha1
kind: Pipeline
metadata:
  name: my-pipeline
  namespace: default
  annotations:
    gpu-direct-comm.numaproj.io/numa-network-edges: |
      [
        {
          "from": "vertex-a",
          "to": "vertex-b",
          "numaNetwork": "my-network",
          "connectionType": "direct"
        }
      ]
spec:
  vertices:
    - name: vertex-a
      ...
    - name: vertex-b
      ...
  edges:
    - from: vertex-a
      to: vertex-b
```

The annotation value is a JSON array. Each element has these fields:

| Field | Description |
|-------|-------------|
| `from` | Source vertex name. Must match an edge in `spec.edges`. |
| `to` | Destination vertex name. Must match an edge in `spec.edges`. |
| `numaNetwork` | Name of the NumaNetwork resource in the same namespace. |
| `connectionType` | Must be `"direct"`. No other values are currently supported. |

The Pipeline ValidatingWebhook rejects the resource if any `(from, to)` pair does not match `spec.edges` or if the referenced NumaNetwork does not exist.

## Development

### Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the manager binary |
| `make test` | Run unit tests with envtest |
| `make test-e2e` | Run end-to-end tests with Kind |
| `make lint` | Run golangci-lint |
| `make manifests` | Generate CRD, RBAC, and webhook YAML |
| `make generate` | Generate deepcopy methods |
| `make install` | Install CRDs into the cluster |
| `make deploy` | Deploy the controller to the cluster |
| `make docker-build` | Build the controller Docker image |
| `make docker-build-webhook-nn` | Build the IPAM webhook provider Docker image |
| `make docker-push` | Push the controller Docker image |

### Project Structure

```
api/v1alpha1/                    # CRD types (NumaNetwork)
cmd/main.go                      # Controller entrypoint
config/
  crd/                           # CRD YAML
  manager/                       # Controller deployment
  webhook/                       # ValidatingWebhook configuration
  certmanager/                   # cert-manager certificate
  rbac/                          # RBAC roles and bindings
  testdata/                      # Test manifests (E2E, webhook validation)
  webhook-whereabouts-numanetwork/  # IPAM webhook provider deployment
internal/
  controller/                    # NumaNetwork reconciler
  webhook/v1alpha1/              # Pipeline ValidatingWebhook handler
  ipam/                          # IPAM profile name parser
test/e2e/                        # End-to-end test suite
docs/diagrams/                   # Architecture diagrams (.drawio source)
```

## Testing

### Unit Tests

Unit tests run against a local control plane provided by [envtest](https://book.kubebuilder.io/reference/envtest):

```bash
make test
```

### End-to-End Tests

E2E tests run on a Kind cluster (created automatically by the target):

```bash
make test-e2e
```

## Roadmap

This project is developed in three major goals (G1–G3).

### G1: NumaNetwork CRD & Secondary NIC Integration

| Milestone | Description |
|---|---|
| M1 | NumaNetwork CRD |
| M2 | NumaNetwork to ResourceClaimTemplate generation |
| M3 | Pipeline edge binding + ValidatingWebhook |
| M4 | Pipeline ResourceClaim field injection |
| M5 | IPAM webhook provider (webhook-whereabouts-numanetwork) |
| M6 | E2E CI |

### G2–G3

Planning in progress.

## Using with Claude Code

This project includes a `CLAUDE.md` that gives Claude Code full context on the codebase, commands, and architecture.

```bash
claude    # Start Claude Code — it reads CLAUDE.md automatically
```

## Contributing

We welcome contributions. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## License

This project is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for the full text.
