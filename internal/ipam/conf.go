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

package ipam

import (
	"encoding/json"
	"fmt"
)

const (
	cniVersion      = "0.3.1"
	whereaboutsIPAM = "whereabouts"
)

// whereaboutsConf is the CNI configuration structure for whereabouts IPAM.
type whereaboutsConf struct {
	CNIVersion string              `json:"cniVersion"`
	Name       string              `json:"name"`
	IPAM       whereaboutsIPAMConf `json:"ipam"`
}

type whereaboutsIPAMConf struct {
	Type  string `json:"type"`
	Range string `json:"range"`
}

// BuildConfFromNuma builds a whereabouts CNI configuration JSON from a
// numaNetwork's ipRange. The ipRange CIDR validity is enforced by the
// NumaNetwork CRD validation marker (numanetwork_types.go:41).
func BuildConfFromNuma(networkName, ipRange string) ([]byte, error) {
	if networkName == "" {
		return nil, fmt.Errorf("networkName must not be empty")
	}
	if ipRange == "" {
		return nil, fmt.Errorf("ipRange must not be empty")
	}
	conf := whereaboutsConf{
		CNIVersion: cniVersion,
		Name:       networkName,
		IPAM: whereaboutsIPAMConf{
			Type:  whereaboutsIPAM,
			Range: ipRange,
		},
	}
	return json.Marshal(conf)
}
