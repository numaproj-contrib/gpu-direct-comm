/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dns

import "context"

// Store manages DNS A records in a backend store (e.g. etcd for CoreDNS).
//
// Each record is keyed by (fqdn, podID), allowing multiple Pods belonging
// to the same Vertex to register distinct A records under a single FQDN.
// CoreDNS returns all records as a round-robin A response.
type Store interface {
	// Put creates or updates an A record for (fqdn, podID) → ip.
	Put(ctx context.Context, fqdn, podID, ip string) error

	// Delete removes the A record for (fqdn, podID).
	Delete(ctx context.Context, fqdn, podID string) error

	// Get returns all IP addresses registered under fqdn.
	// Returns an empty slice and no error if no records exist.
	Get(ctx context.Context, fqdn string) ([]string, error)
}
