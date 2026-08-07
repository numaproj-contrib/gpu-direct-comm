# hack/

Shell scripts for manual E2E verification on k3d or baremetal clusters.
These scripts complement the automated unit/integration tests (`make test`)
by exercising the full Kubernetes deployment flow.

All scripts require a running cluster with the prerequisites listed in
[docs/setup-guide.ja.md](../docs/setup-guide.ja.md).

## Scripts

| Script | Purpose | Milestones |
|--------|---------|------------|
| `e2e-coredns-etcd.sh` | Verify CoreDNS etcd plugin: register a test DNS record via etcdctl, resolve it from a test Pod, delete and verify NXDOMAIN | G2-M2 |
| `e2e-webhook-whereabouts.sh` | Verify webhook-whereabouts-numanetwork: deploy a Pipeline with a gpu-direct edge, verify IP assignment on secondary NIC, verify IP release on deletion | G1 |
| `e2e-full-flow.sh` | Integrated M1–M6 verification: vertexDomain annotation injection, DNS record CRUD, name resolution, round-robin DNS, cleanup on deletion. Use `--env local\|baremetal` to select environment | G2-M7 |

## Usage

```bash
# CoreDNS etcd plugin verification (k3d)
./hack/e2e-coredns-etcd.sh

# Webhook + IP assignment verification (k3d)
./hack/e2e-webhook-whereabouts.sh

# Full-flow integrated E2E (k3d with dummy interfaces)
./hack/e2e-full-flow.sh --env local

# Full-flow integrated E2E (baremetal with SR-IOV VFs)
./hack/e2e-full-flow.sh --env baremetal
```

Equivalent Makefile targets are available for the full-flow tests:

```bash
make test-e2e-full-local
make test-e2e-full-baremetal
```
