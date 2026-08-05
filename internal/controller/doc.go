// Package controller reconciles Kubernetes resources for GPU direct
// communication.
//
// NumaNetworkReconciler creates a ResourceClaimTemplate with a dranet
// opaque config embedding the profile name (<namespace>/<name>) for each
// NumaNetwork. Ownership is tracked via ownerReference so that deleting
// a NumaNetwork garbage-collects the corresponding RCT.
//
// VertexDomainReconciler watches Pods with the vertexDomain label,
// extracts the Second NIC IP from ResourceClaim status, and registers
// DNS A records in the CoreDNS etcd backend via dns.Store. A finalizer
// guarantees DNS cleanup on Pod deletion.
package controller
