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

// Package ipam implements the custom webhook provider for dranet BYODP,
// resolving numaNetwork ipRange to whereabouts IP assignments.
//
// Wire types in this file mirror the JSON contract used by dranet's webhook
// protocol (sigs.k8s.io/dranet/pkg/cloudprovider/webhook and pkg/apis).
// We define them locally because dranet v1.3.0 does not yet export the
// BYODP webhook types (ProfileRequest, Capabilities), and the exported
// types have incompatible JSON tags or missing fields (see ADR-0011).
package ipam

// ProfileRequest is the JSON body dranet sends to /GetProfileConfig and
// /ReleaseProfileConfig. Mirrors sigs.k8s.io/dranet/pkg/cloudprovider/webhook.ProfileRequest.
type ProfileRequest struct {
	Device   DeviceIdentifiers `json:"device"`
	ClaimUID string            `json:"claim_uid"`
	Config   *NetworkConfig    `json:"config,omitempty"`
}

// DeviceIdentifiers identifies the NIC device.
// Mirrors sigs.k8s.io/dranet/pkg/cloudprovider.DeviceIdentifiers.
type DeviceIdentifiers struct {
	MAC        string `json:"mac,omitempty"`
	PCIAddress string `json:"pciAddress,omitempty"`
	Name       string `json:"name,omitempty"`
}

// NetworkConfig carries the profile name and interface configuration.
// Mirrors sigs.k8s.io/dranet/pkg/apis.NetworkConfig.
type NetworkConfig struct {
	Profile   string          `json:"profile,omitempty"`
	Interface InterfaceConfig `json:"interface"`
}

// InterfaceConfig represents network interface configuration returned to dranet.
// Mirrors sigs.k8s.io/dranet/pkg/apis.InterfaceConfig (subset).
type InterfaceConfig struct {
	Addresses []string `json:"addresses,omitempty"`
}

// Capabilities is the response for the /health endpoint capability negotiation.
// Mirrors sigs.k8s.io/dranet/pkg/cloudprovider/webhook.Capabilities.
type Capabilities struct {
	CloudProvider   bool `json:"cloudProvider"`
	ProfileProvider bool `json:"profileProvider"`
}
