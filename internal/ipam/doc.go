// Package ipam implements a dranet BYODP webhook provider.
//
// It resolves the profile name (set by internal/controller) back to
// a NumaNetwork, reads its ipRange, and delegates IP allocation to
// the whereabouts CNI binary. This runs as a separate binary
// (cmd/webhook-whereabouts-numanetwork), not inside the controller-manager.
//
// Endpoints:
//   - POST /GetProfileConfig  -- allocate IP via whereabouts ADD
//   - POST /ReleaseProfileConfig -- release IP via whereabouts DEL
//   - GET  /health -- capability advertisement
package ipam
