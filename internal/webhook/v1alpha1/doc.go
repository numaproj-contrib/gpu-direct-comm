// Package v1alpha1 provides a ValidatingWebhook for numaflow Pipeline.
//
// When a Pipeline carries the annotation
// "gpu-direct-comm.numaproj.io/numa-network-edges", this webhook
// validates that each binding references an existing spec.edge and
// a NumaNetwork in the same namespace. Pipelines without the
// annotation pass through unchanged (non-breaking).
//
// VertexDomainMutator is a MutatingWebhook for Pod CREATE that injects
// a vertexDomain marker label and FQDN annotation into Vertex Pods
// participating in a numaNetwork direct binding. The label serves as a
// predicate filter; the FQDN (stored in an annotation per ADR-003)
// follows the format <vertex>.<pipeline>.<namespace>.vertexdomain.local
// and is consumed by VertexDomainReconciler for DNS record registration.
//
// All webhooks run inside the controller-manager process and are
// registered on the manager's webhook server (port 9443).
package v1alpha1
