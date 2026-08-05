// Package controller reconciles NumaNetwork resources and manages
// vertexDomain DNS records.
//
// NumaNetworkReconciler creates a ResourceClaimTemplate for each
// NumaNetwork with a dranet opaque config embedding the profile name
// (<namespace>/<name>). Ownership is tracked via ownerReference so
// that deleting a NumaNetwork garbage-collects the corresponding RCT.
//
// VertexDomainReconciler watches Pods with the vertexDomain label,
// extracts the Secondary NIC IP from the associated ResourceClaim,
// and registers/removes DNS A records in the CoreDNS etcd backend.
package controller
