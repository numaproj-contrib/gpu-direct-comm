// Package v1alpha1 provides a ValidatingWebhook for numaflow Pipeline.
//
// When a Pipeline carries the annotation
// "gpu-direct-comm.numaproj.io/numa-network-edges", this webhook
// validates that each binding references an existing spec.edge and
// a NumaNetwork in the same namespace. Pipelines without the
// annotation pass through unchanged (non-breaking).
//
// The webhook runs inside the controller-manager process and is
// registered on the manager's webhook server (port 9443).
package v1alpha1
