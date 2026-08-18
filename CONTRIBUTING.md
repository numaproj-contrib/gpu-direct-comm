# Contributing to gpu-direct-comm

> This is the English translation of [CONTRIBUTING.ja.md](./CONTRIBUTING.ja.md). In case of discrepancies, the Japanese version is authoritative.

Thank you for your interest in contributing to gpu-direct-comm. This document explains how to get started and what to expect from the contribution process.

## Table of Contents

- [Reporting Issues](#reporting-issues)
- [Development Setup](#development-setup)
- [Code Style](#code-style)
- [Make Targets](#make-targets)
- [Unit Tests](#unit-tests)
- [E2E Tests](#e2e-tests)
- [Commit Messages](#commit-messages)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Using Claude Code](#using-claude-code)
- [Code of Conduct](#code-of-conduct)

## Reporting Issues

Before opening a new issue, please search the existing issues to avoid duplicates.

When reporting a bug, include:

- A clear and descriptive title
- Steps to reproduce the problem
- Expected behavior and actual behavior
- Kubernetes version (`kubectl version`)
- Go version (`go version`)
- Relevant logs or error messages

## Development Setup

See the [Environment Setup](docs/setup-guide.md) guide for step-by-step instructions on building a local k3d cluster or a bare-metal cluster.

## Code Style

- Format code with `gofmt` or `goimports` before committing.
- Run `make lint` and fix all warnings before submitting a PR.
- Follow idiomatic Go patterns. See [Effective Go](https://go.dev/doc/effective_go) for reference.
- Keep functions short and focused (under 50 lines when possible).
- Use meaningful names for variables, functions, and types.
- Accept interfaces, return structs.
- Always wrap errors with context using `fmt.Errorf("...: %w", err)`.
- Do not add features or abstractions that are not needed yet (YAGNI).

When modifying or adding a package, update the corresponding `doc.go` file to keep the package-level documentation accurate.

## Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the manager binary |
| `make test` | Run unit tests with envtest |
| `make test-e2e` | Run e2e tests with k3d |
| `make test-e2e-full-local` | Run full-flow E2E tests on local k3d cluster (M1–M6) |
| `make test-e2e-full-baremetal` | Run full-flow E2E tests on baremetal cluster (SR-IOV VFs) |
| `make lint` | Run golangci-lint |
| `make lint-fix` | Run golangci-lint and apply automatic fixes |
| `make manifests` | Regenerate CRD, RBAC, and webhook YAML |
| `make generate` | Regenerate deepcopy methods |
| `make fmt` | Run `go fmt` on all packages |
| `make vet` | Run `go vet` on all packages |

## Unit Tests

Unit tests run against a local control plane provided by [envtest](https://book.kubebuilder.io/reference/envtest). No running cluster or Docker is required — `internal/ipam` and `internal/controller` tests use fake Kubernetes clients (`sigs.k8s.io/controller-runtime/pkg/client/fake`) and stub binaries instead of a real cluster. `make test` downloads the `envtest` binaries (`setup-envtest`) automatically on first run — this needs internet access once, but not a running cluster.

```bash
go mod download
make test
```

### Testing Guidelines

- Write tests for all new functionality before writing the implementation (TDD).
- Use [envtest](https://book.kubebuilder.io/reference/envtest) for controller and webhook tests.
- Aim for 80% or higher test coverage across the packages you change.
- Use the AAA (Arrange-Act-Assert) structure for test readability.
- Use descriptive test function names that explain the behavior under test.

```go
func TestReconcile_CreatesRCT_WhenNumaNetworkIsCreated(t *testing.T) {
    // Arrange
    nn := buildNumaNetwork("default", "my-network", "vf.nvidia.dra.net", "192.168.10.0/24")

    // Act
    result, err := reconciler.Reconcile(ctx, requestFor(nn))

    // Assert
    require.NoError(t, err)
    assert.Equal(t, ctrl.Result{}, result)
}
```

Run the full test suite before submitting:

```bash
make test
make lint
```

## E2E Tests

### Local Cluster

E2E tests validate the following two goals end to end:

- **Goal 1 (IP assignment)**: A `dummy0` interface is created on every k3d node. DRANET publishes it as an allocatable device. When a Numaflow Pipeline with a `connectionType: direct` edge is deployed, `webhook-whereabouts-numanetwork` assigns an IP from `ipRange` via `whereabouts`. The Mutating Webhook injects the ResourceClaimTemplate into both vertices of the edge, so both vertex Pods receive a Secondary NIC. Real SR-IOV VF hardware is not required — `dummy0` stands in for a real Secondary NIC (dranet's own upstream E2E tests use the same technique).
- **Goal 2 (DNS resolution)**: vertexDomainMutator injects a FQDN annotation on Pods. vertexDomainController registers DNS records in CoreDNS etcd. The test verifies that destination Pod IPs can be resolved via DNS from within the pipeline.

**Prerequisite**: Complete the [Local Cluster](docs/setup-guide.md#1-local-cluster) environment setup first — all components (`whereabouts`, DRANET, `dranet` DeviceClass, controller manager, `webhook-whereabouts-numanetwork`, CoreDNS etcd backend) must be deployed and `READY`. Make sure unit tests (`make test`) pass before running E2E tests.

To run all steps at once:

```bash
# Goal 1 + Goal 2 full flow
make test-e2e-full-local
# Or: ./hack/e2e-full-flow.sh --env local

# Goal 1 (IP assignment) only
# ./hack/e2e-webhook-whereabouts.sh
```

The individual steps below explain what the script does. Environment setup (dummy interface creation, DeviceClass, BYODP webhook configuration, etc.) is assumed to be complete — all checks in [setup-guide.md](docs/setup-guide.md#1-local-cluster) must pass.

#### 1. Deploy the Pipeline (NumaNetwork + ISBSvc + Pipeline)

`e2e_full_flow_local.yaml` bundles a NumaNetwork and a Pipeline with a `connectionType: direct` edge. NumaNetworkReconciler creates a RCT (`<numaNetworkName>-rct`), and the Pipeline Mutating Webhook injects the RCT into vertices participating in direct binding:

```bash
kubectl apply -f config/testdata/e2e_full_flow_local.yaml
kubectl get resourceclaimtemplate e2e-full-flow-nn-rct   # created by the controller
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline,app.kubernetes.io/component=vertex --timeout=120s
```

#### 2. Verify IP assignment from ipRange

The DRA ResourceClaim `status.devices[].networkData` contains the network information written by the DRANET driver after device allocation. This approach works even when the container image does not include network tools (`ip`, `ls`, etc.):

```bash
# Filter by vertex-domain label (same entry point as the controller)
for pod in $(kubectl get pods -l gpu-direct-comm.numaproj.io/vertex-domain=true,numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline -o name); do
  pod_name=$(echo "$pod" | sed 's|pod/||')
  node=$(kubectl get "$pod" -o jsonpath='{.spec.nodeName}')
  echo "=== $pod_name (node: $node) ==="
  # resourceClaimStatuses[] — list of ResourceClaims bound to this Pod
  for claim in $(kubectl get "$pod" -o jsonpath='{.status.resourceClaimStatuses[*].resourceClaimName}'); do
    echo "  Claim: $claim"
    # devices[]          — each allocated device in the claim
    # networkData.ips[]  — IP addresses assigned by the IPAM provider (whereabouts)
    # networkData.interfaceName      — NIC name inside the Pod (e.g. dummy0, enp4s0f0v0)
    # networkData.hardwareAddress    — MAC address of the NIC
    kubectl get resourceclaim "$claim" -o jsonpath='{range .status.devices[*]}    Interface: {.networkData.interfaceName}  MAC: {.networkData.hardwareAddress}  IPs: {.networkData.ips[*]}{"\n"}{end}'
  done
done
# Expected: vertex Pods participating in direct binding have an IP within 192.168.140.0/24 on dummy0
```

#### 3. Verify FQDN records in etcd

vertexDomainController registers the Secondary NIC IP of Pods with the `vertex-domain=true` label into CoreDNS etcd. The FQDN format is `<vertex>.<pipeline>.<namespace>.vertexdomain.local`.

```bash
# List DNS record keys in etcd
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --keys-only
# Expected: one key per Pod for both in and out vertices
#   /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/in/<pod-id>
#   /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/out/<pod-id>

# Check the value of each record (Secondary NIC IP of each Pod)
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --print-value-only
# Expected: each record is a JSON object like {"host":"192.168.140.x"}
```

#### 4. Verify DNS resolution of destination vertex from within the pipeline

Resolve the destination (To: `out`) vertex FQDN from the source (From: `in`) side and verify that destination Pod IPs are returned (ADR-004: single-direction communication). The `out` vertex has `scale.min: 2`, so multiple IPs are returned from the same FQDN (round-robin).

```bash
# Start a temporary Pod for DNS verification
kubectl run e2e-dns-test --image=busybox:1.36 --restart=Never -- sleep 3600
kubectl wait --for=condition=Ready pod/e2e-dns-test --timeout=30s

# Resolve the out vertex (destination) FQDN (2 Pods -> 2 IPs, round-robin)
# In direct communication, the From side resolves the To side's FQDN to get destination IPs
kubectl exec e2e-dns-test -- nslookup out.e2e-full-flow-pipeline.default.vertexdomain.local
# Expected: Address lines contain one or more IPs within 192.168.140.x

# Do not delete the test Pod yet — it is used in step 5
```

#### 5. Delete the Pipeline and verify resource cleanup

Delete the Pipeline and NumaNetwork, then verify that all DNS records and IP addresses are cleaned up:

```bash
# Delete the Pipeline and NumaNetwork
kubectl delete -f config/testdata/e2e_full_flow_local.yaml --wait=true --timeout=60s

# Verify DNS records are deleted from etcd
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --keys-only
# Expected: no output (all records deleted)

# Verify nslookup returns NXDOMAIN
kubectl exec e2e-dns-test -- nslookup out.e2e-full-flow-pipeline.default.vertexdomain.local
# Expected: NXDOMAIN

# Verify whereabouts IP pool allocations are released
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# Expected: empty ({})

# Delete the test Pod
kubectl delete pod e2e-dns-test
```

> Steps 1–5 above are currently a manual walkthrough, not an automated test target. `make test-e2e` (`test/e2e/`) is the kubebuilder-scaffolded generic suite — it spins up its own Kind cluster and does not exercise DRANET, whereabouts, or `NumaNetwork` at all. Do not run it expecting it to cover the flow described in this section.

### Bare-metal Cluster

E2E validation on bare-metal verifies the same Goal 1 (IP assignment) + Goal 2 (DNS resolution) as the [Local Cluster](#local-cluster) above. The differences from the Local Cluster are:

- No `dummy0` interface is needed — real SR-IOV VFs serve as the Secondary NIC.
- IP verification uses the DRA ResourceClaim `networkData` instead of `docker exec`.

**Prerequisite**: Complete the [Bare-metal Cluster](docs/setup-guide.md#2-bare-metal-cluster) environment setup first, and make sure all checks pass.

To run all steps at once:

```bash
# Goal 1 + Goal 2 full flow
make test-e2e-full-baremetal
# Or: ./hack/e2e-full-flow.sh --env baremetal
```

The individual steps below explain what the script does.

#### 1. Deploy the Pipeline (NumaNetwork + ISBSvc + Pipeline)

`config/testdata/e2e_full_flow_baremetal.yaml` uses `ipRange: "192.168.140.0/24"`. This assumes no real network on your hardware already occupies that range. If it conflicts with your environment, adjust `NumaNetwork.spec.refResourceClaimDranet.ipRange` in a copy of the manifest:

```bash
kubectl apply -f config/testdata/e2e_full_flow_baremetal.yaml
kubectl get resourceclaimtemplate e2e-full-flow-nn-rct   # created by the controller
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline,app.kubernetes.io/component=vertex --timeout=120s
```

If Pods stay `Pending`, check events for ResourceClaim allocation failures — a common cause is the DeviceClass not matching any VF devices:

```bash
kubectl describe pod -l numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline | grep -A5 Events
```

#### 2. Verify IP assignment from ipRange

Same as [Local Cluster step 2](#2-verify-ip-assignment-from-iprange).

The Secondary NIC interface name depends on your hardware (e.g. `enp4s0f0v0`). It is shown in the `Interface` field of the output.

Then verify that the IP is bound to a real SR-IOV VF, not a dummy device. The `networkData.interfaceName` field in the ResourceClaim reports the actual interface name inside the Pod:

```bash
for pod in $(kubectl get pods -l gpu-direct-comm.numaproj.io/vertex-domain=true,numaflow.numaproj.io/pipeline-name=e2e-full-flow-pipeline -o name); do
  pod_name=$(echo "$pod" | sed 's|pod/||')
  echo "=== $pod_name ==="
  for claim in $(kubectl get "$pod" -o jsonpath='{.status.resourceClaimStatuses[*].resourceClaimName}'); do
    kubectl get resourceclaim "$claim" \
      -o jsonpath='  Interface: {.status.devices[0].networkData.interfaceName}  IPs: {.status.devices[0].networkData.ips[*]}{"\n"}'
  done
done
# Expected: each Pod shows a VF interface name (e.g. enp86s0f0v0), not "dummy0"
```

#### 3. Verify FQDN records in etcd

Same as [Local Cluster step 3](#3-verify-fqdn-records-in-etcd).

#### 4. Verify DNS resolution of destination vertex from within the pipeline

Same as [Local Cluster step 4](#4-verify-dns-resolution-of-destination-vertex-from-within-the-pipeline).

#### 5. Delete the Pipeline and verify resource cleanup

Delete the Pipeline and NumaNetwork, then verify that all DNS records and IP addresses are cleaned up:

```bash
# Delete the Pipeline and NumaNetwork
kubectl delete -f config/testdata/e2e_full_flow_baremetal.yaml --wait=true --timeout=60s

# Verify DNS records are deleted from etcd
kubectl -n kube-system exec etcd-coredns-0 -- \
  etcdctl get --prefix /skydns/local/vertexdomain/default/e2e-full-flow-pipeline/ --keys-only
# Expected: no output (all records deleted)

# Verify nslookup returns NXDOMAIN
kubectl exec e2e-dns-test -- nslookup out.e2e-full-flow-pipeline.default.vertexdomain.local
# Expected: NXDOMAIN

# Verify whereabouts IP pool allocations are released
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# Expected: empty ({})

# Delete the test Pod
kubectl delete pod e2e-dns-test
```

> As with the Local Cluster, this is currently a manual walkthrough, not an automated CI target.

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <description>

<optional body>

Signed-off-by: Your Name <your.email@example.com>
```

### Types

| Type | When to Use |
|------|-------------|
| `feat` | A new feature |
| `fix` | A bug fix |
| `refactor` | A code change that does not add a feature or fix a bug |
| `test` | Adding or updating tests |
| `docs` | Documentation changes |
| `chore` | Build process, tooling, or dependency updates |
| `perf` | Performance improvements |
| `ci` | CI/CD configuration changes |

### DCO Sign-off

All commits must include a DCO (Developer Certificate of Origin) sign-off. Use the `-s` flag when committing:

```bash
git commit -s -m "feat(controller): add health check endpoint"
```

This adds a `Signed-off-by` line to your commit message. It certifies that you wrote the code or have the right to submit it under the project license. See [developercertificate.org](https://developercertificate.org/) for the full text.

Commits without a sign-off will not be merged.

## Pull Request Guidelines

- Submit all pull requests to the **`develop`** branch, not `main`.
- Keep PRs focused on a single change. Do not mix unrelated changes in one PR.
- Write a clear title (under 70 characters).
- Make sure `make test` and `make lint` pass before requesting review.
- Resolve merge conflicts before requesting review.

### PR Description Template

```markdown
## Summary
- What does this PR do?
- Why is this change needed?

## Test Plan
- [ ] Unit tests added or updated
- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] (if applicable) E2E tests pass
```

Write the PR description in plain English. Clear and simple sentences are better than complex phrasing. Non-native English speakers are welcome and valued contributors.

## Using Claude Code

This project includes a `CLAUDE.md` that gives Claude Code full context on the codebase, commands, and architecture.

```bash
claude    # Start Claude Code in the project root
```

Claude Code reads `CLAUDE.md` automatically and can help with implementing features, writing tests, and navigating the codebase.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you agree to uphold this standard. Please report unacceptable behavior to the project maintainers.
