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
type Store interface {
	// Put creates or updates an A record mapping fqdn to ip.
	Put(ctx context.Context, fqdn string, ip string) error

	// Delete removes the A record for fqdn.
	Delete(ctx context.Context, fqdn string) error

	// Get returns the IP address associated with fqdn.
	// Returns an empty string and no error if the record does not exist.
	Get(ctx context.Context, fqdn string) (string, error)
}
