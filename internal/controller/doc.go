// Package controller reconciles NumaNetwork resources.
//
// For each NumaNetwork it creates a ResourceClaimTemplate with
// a dranet opaque config embedding the profile name (<namespace>/<name>).
// Ownership is tracked via ownerReference so that deleting a
// NumaNetwork garbage-collects the corresponding RCT.
package controller
