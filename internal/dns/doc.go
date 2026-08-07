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

// Package dns provides a Store interface for managing DNS A records
// in the CoreDNS etcd backend.
//
// The primary implementation, EtcdStore, writes SkyDNS-compatible JSON
// records to etcd v3. Each record is keyed by (FQDN, podID) using
// sub-keys under the SkyDNS path (e.g. /skydns/.../vertex-name/<podID>),
// allowing multiple Pods of the same Vertex to register distinct A records.
// CoreDNS returns all sub-key records as a round-robin A response.
//
// The vertexDomainController uses this package to register and remove
// DNS entries for Secondary NIC IPs.
package dns
