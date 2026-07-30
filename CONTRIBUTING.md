# Contributing to gpu-direct-comm

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

E2E tests validate `NumaNetwork.spec.refResourceClaimDranet.ipRange` end to end: a `dummy0` interface is created on every k3d node, DRANET publishes it as an allocatable device, and `webhook-whereabouts-numanetwork` assigns it an IP from `ipRange` via `whereabouts` when a Numaflow Pipeline with `connectionType: direct` edges is deployed. The Mutating Webhook injects the ResourceClaimTemplate into both vertices of the edge, so both vertex Pods receive a secondary NIC. Real SR-IOV VF hardware is not required — `dummy0` stands in for a real Secondary NIC (dranet's own upstream E2E tests use the same technique).

**Prerequisite**: Complete the [Local Cluster](docs/setup-guide.md#1-local-cluster) environment setup first — all components (`whereabouts`, DRANET, `dranet` DeviceClass, controller manager, `webhook-whereabouts-numanetwork`) must be deployed and `READY`. Unit tests (`make test`) must pass before running E2E tests.

To run all steps at once, use the helper script:

```bash
./hack/e2e-webhook-whereabouts.sh
```

The individual steps below explain what the script does.

#### 1. Create a dummy interface on every node

```bash
for node in k3d-numaflow-cluster-server-0 k3d-numaflow-cluster-agent-0 k3d-numaflow-cluster-agent-1; do
  docker exec "$node" sh -c "ip link show dummy0 >/dev/null 2>&1 || (ip link add dummy0 type dummy && ip link set up dev dummy0)"
done
```

Confirm DRANET picked it up (look for `dra.net/type: dummy` in the output):

```bash
kubectl get resourceslice -o yaml | grep -A2 'ifName: dummy0'
```

#### 2. Verify the DeviceClass is deployed

Confirm that the dummy-specific DeviceClass from the setup guide is present:

```bash
kubectl get deviceclass dranet-e2e-local
# Expected: the DeviceClass with AGE
```

#### 3. Configure DRANET for BYODP webhook integration

This step configures DRANET to delegate IPAM to `webhook-whereabouts-numanetwork` (the webhook built in this project). The patch makes three kinds of changes:

- **Webhook args (always required)**: `--profile-provider=webhook` and `--webhook-url` tell DRANET to call the webhook for IP assignment on every NIC allocation. These are permanent settings for any environment using gpu-direct-comm.
- **dnsPolicy (always required)**: DRANET runs with `hostNetwork: true`, so the default `dnsPolicy: Default` resolves DNS via the host's resolver, which cannot resolve cluster-internal `.svc` names. `ClusterFirstWithHostNet` fixes this.
- **Image swap (temporary)**: as of this writing, the official `registry.k8s.io/networking/dranet:stable` tag is built from `v1.3.0` (released 2026-05-28), which predates the BYODP webhook feature (merged in [dranet PR #223](https://github.com/kubernetes-sigs/dranet/pull/223) on 2026-06-10). Until an official release includes it, use the CI-built image:

```bash
docker pull gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5
k3d image import gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5 -c numaflow-cluster
```

> Before reusing this pinned tag on a future date, check whether an official release now includes BYODP: `crane ls registry.k8s.io/networking/dranet` and check the [dranet releases page](https://github.com/kubernetes-sigs/dranet/releases) for a version after PR #223. If one exists, use the official `stable` tag instead and skip the `docker pull`/`k3d image import` above.

Apply the patch:

```bash
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--profile-provider=webhook"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--webhook-url=http://webhook-whereabouts-numanetwork.kube-system.svc:8443"}
]'
kubectl -n kube-system rollout status ds/dranet --timeout=90s
```

> dranet fails fast (`Fatal`, immediate crash) if it cannot reach `--webhook-url`'s `/health` endpoint at startup. This is why `webhook-whereabouts-numanetwork` must already be deployed and `READY` *before* this step — switching dranet to webhook mode first, then deploying the webhook, will crash-loop.

#### 4. Deploy the Pipeline (NumaNetwork + ISBSvc + Pipeline)

`e2e_ip_assign_local.yaml` bundles a NumaNetwork, an InterStepBufferService, and a Pipeline with a `connectionType: direct` edge. The Mutating Webhook injects `e2e-numanetwork-rct` into both the `in` (source) and `out` (sink) vertices:

```bash
kubectl apply -f config/testdata/e2e_ip_assign_local.yaml
kubectl get resourceclaimtemplate e2e-numanetwork-rct   # created by the controller
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline --timeout=120s
```

#### 5. Verify the IP was assigned from ipRange

The DRA ResourceClaim `status.devices[].networkData` contains the network information
populated by the DRANET driver after device allocation. This approach works regardless
of whether the container image includes network tools (`ip`, `ls`, etc.):

```bash
for pod in $(kubectl get pods -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline -o name | grep -E 'in-|out-'); do
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
# Expect an IP inside 192.168.140.0/24 on dummy0 for both in and out vertex Pods
```

#### 6. Verify IPs are released on Pipeline deletion

```bash
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
kubectl delete pipeline e2e-gpu-direct-pipeline
sleep 5
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# Expect the allocations map to become empty ({}) after deletion
```

#### Cleanup

```bash
kubectl delete -f config/testdata/e2e_ip_assign_local.yaml
```

To fully revert DRANET to its pre-test state (remove webhook args, restore dnsPolicy, and restore the official image), run the following. Note that in a production environment you would keep the webhook args and dnsPolicy — only the image swap is temporary:

```bash
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"registry.k8s.io/networking/dranet:stable"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"Default"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/4"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/3"}
]'
```

> Steps 1–6 above are currently a manual walkthrough, not an automated test target. `make test-e2e` (`test/e2e/`) is the kubebuilder-scaffolded generic suite — it spins up its own Kind cluster and does not exercise DRANET, whereabouts, or `NumaNetwork` at all. Do not run it expecting it to cover the flow described in this section.

### Bare-metal Cluster

E2E validation on bare-metal follows the same flow as the [Local Cluster](#local-cluster) above — a NumaNetwork-annotated Pipeline is deployed and both vertex Pods must receive an IP from `NumaNetwork.spec.refResourceClaimDranet.ipRange` on their Secondary NIC (an SR-IOV VF). The differences from the Local Cluster are:

- No `dummy0` interface is needed — real SR-IOV VFs serve as the Secondary NIC.
- The narrowed E2E `DeviceClass` (`config/testdata/e2e_deviceclass_dranet_local.yaml`) is not required unless your hardware also publishes non-NIC devices through DRANET.
- The DRANET pinned image must be pulled from a registry (no `k3d image import`).
- IP verification uses SSH + `nsenter` instead of `docker exec`.

**Prerequisite**: Complete the [Bare-metal Cluster](docs/setup-guide.md#2-bare-metal-cluster) environment setup first — including SR-IOV VF preparation, the cluster/GPU/DRA/Numaflow layer via `numaflow-dra-ansible`, then DRANET, the `dranet` DeviceClass, whereabouts, cert-manager, and gpu-direct-comm components (CRD, controller manager, `webhook-whereabouts-numanetwork`) must all be deployed and `READY`.

#### 0. Verify SR-IOV VFs are recognized by DRANET

Before starting the E2E flow, confirm that DRANET has detected the SR-IOV VFs on each worker node.

DRANET publishes every NIC on a node as a device inside a `ResourceSlice` object (one per node per driver). Each device carries a set of `dra.net/*` attributes. DRANET sets `dra.net/isSriovVf: true` exclusively on VFs; PFs, non-SR-IOV physical NICs, and software interfaces lack this attribute entirely.

Run the following command to list only VF devices across all nodes:

```bash
kubectl get resourceslices -o json | jq -r '
  .items[]
  | select(.spec.driver == "dra.net")
  | .spec.nodeName as $node
  | .spec.devices[]
  | select(.attributes["dra.net/isSriovVf"].bool == true)
  | "\($node): \(.attributes["dra.net/ifName"].string) (PCI: \(.attributes["dra.net/pciAddress"].string))"
'
```

You should see one line per VF on each worker node. If no VF entries appear, revisit [SR-IOV VF preparation](docs/setup-guide.md#1-sr-iov-vf-preparation) in the setup guide.

Also confirm the VF-specific DeviceClass from the setup guide is deployed:

```bash
kubectl get deviceclass dranet-e2e-baremetal
# Expected: the DeviceClass with AGE
```

If the DeviceClass does not exist, revisit the [DeviceClass creation](docs/setup-guide.md#3-deviceclass-creation) step in the setup guide.

#### 1. Configure DRANET for BYODP webhook integration

Same as [Local Cluster step 3](#3-configure-dranet-for-byodp-webhook-integration), with one difference: bare-metal nodes pull images from a registry, not via `k3d image import`. Ensure the pinned DRANET image is available in a registry your nodes can reach:

```bash
# If your nodes can pull directly from gcr.io:
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"gcr.io/k8s-staging-networking/dranet:v1.3.0-29-g1b7c7e5"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--profile-provider=webhook"},
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--webhook-url=http://webhook-whereabouts-numanetwork.kube-system.svc:8443"}
]'
kubectl -n kube-system rollout status ds/dranet --timeout=90s
```

If your nodes cannot reach `gcr.io`, mirror the image to your private registry first (`docker pull` + `docker tag` + `docker push`), then use the mirrored image reference in the patch above.

> As with the Local Cluster, check whether an official DRANET release now includes BYODP before reusing this pinned tag — see [Local Cluster step 3](#3-configure-dranet-for-byodp-webhook-integration) for the rationale and how to check.

#### 2. Deploy the Pipeline (NumaNetwork + ISBSvc + Pipeline)

`config/testdata/e2e_ip_assign_baremetal.yaml` uses `ipRange: "192.168.140.0/24"`, which assumes no real network on your hardware already occupies that range. Adjust the `NumaNetwork.spec.refResourceClaimDranet.ipRange` in a copy of the manifest if it conflicts with your environment:

```bash
kubectl apply -f config/testdata/e2e_ip_assign_baremetal.yaml
kubectl get resourceclaimtemplate e2e-numanetwork-rct   # created by the controller
kubectl wait --for=condition=Ready pod -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline --timeout=120s
```

If Pods stay `Pending`, check events for ResourceClaim allocation failures — a common cause is the DeviceClass not matching any VF devices:

```bash
kubectl describe pod -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline | grep -A5 Events
```

#### 3. Verify the IP was assigned from ipRange

The DRA ResourceClaim `status.devices[].networkData` contains the network information
populated by the DRANET driver after device allocation. This approach does not require
SSH access or `sudo` privileges on the bare-metal nodes:

```bash
for pod in $(kubectl get pods -l numaflow.numaproj.io/pipeline-name=e2e-gpu-direct-pipeline -o name | grep -E 'in-|out-'); do
  pod_name=$(echo "$pod" | sed 's|pod/||')
  node=$(kubectl get "$pod" -o jsonpath='{.spec.nodeName}')
  echo "=== $pod_name (node: $node) ==="
  # resourceClaimStatuses[] — list of ResourceClaims bound to this Pod
  for claim in $(kubectl get "$pod" -o jsonpath='{.status.resourceClaimStatuses[*].resourceClaimName}'); do
    echo "  Claim: $claim"
    # devices[]          — each allocated device in the claim
    # networkData.ips[]  — IP addresses assigned by the IPAM provider (whereabouts)
    # networkData.interfaceName      — NIC name inside the Pod (e.g. enp4s0f0v0)
    # networkData.hardwareAddress    — MAC address of the NIC
    kubectl get resourceclaim "$claim" -o jsonpath='{range .status.devices[*]}    Interface: {.networkData.interfaceName}  MAC: {.networkData.hardwareAddress}  IPs: {.networkData.ips[*]}{"\n"}{end}'
  done
done
# Expect an IP inside NumaNetwork.spec.refResourceClaimDranet.ipRange on the Secondary NIC of both in and out vertex Pods
```

The Secondary NIC interface name depends on your hardware (e.g. `enp4s0f0v0`). It is shown in the `Interface` field of the output above.

#### 4. Verify IPs are released on Pipeline deletion

Same as [Local Cluster step 6](#6-verify-ips-are-released-on-pipeline-deletion):

```bash
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
kubectl delete pipeline e2e-gpu-direct-pipeline
sleep 5
kubectl get ippools.whereabouts.cni.cncf.io -A -o jsonpath='{.items[0].spec.allocations}'
# Expect the allocations map to become empty ({}) after deletion
```

#### Cleanup

```bash
kubectl delete -f config/testdata/e2e_ip_assign_baremetal.yaml
```

To fully revert DRANET to its pre-test state (remove webhook args, restore dnsPolicy, and restore the official image), run the following. Note that in a production environment you would keep the webhook args and dnsPolicy — only the image swap is temporary:

```bash
kubectl -n kube-system patch ds dranet --type=json -p='[
  {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"registry.k8s.io/networking/dranet:stable"},
  {"op":"replace","path":"/spec/template/spec/dnsPolicy","value":"Default"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/4"},
  {"op":"remove","path":"/spec/template/spec/containers/0/args/3"}
]'
```

> As on the Local Cluster, this is currently a manual walkthrough, not an automated CI target.

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
