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

package v1alpha1

import (
	"encoding/json"
	"fmt"
)

const (
	// AnnotationNumaNetworkEdges is the annotation key on a numaflow Pipeline
	// that declares edge-to-numaNetwork bindings (ADR-0005).
	AnnotationNumaNetworkEdges = "gpu-direct-comm.numaproj.io/numa-network-edges"

	// ConnectionTypeDirect is the only accepted connectionType value in M3.
	// multi-isbsvc is reserved for a future milestone and is rejected here.
	ConnectionTypeDirect = "direct"
)

// EdgeNumaNetworkBinding represents a single edge-to-numaNetwork binding declared
// in the Pipeline annotation.
type EdgeNumaNetworkBinding struct {
	From           string `json:"from"`
	To             string `json:"to"`
	NumaNetwork    string `json:"numaNetwork"`
	ConnectionType string `json:"connectionType"`
}

// PipelineEdge is a lightweight representation of one entry in spec.edges of a
// numaflow Pipeline, used for binding validation.
type PipelineEdge struct {
	From string
	To   string
}

// ParseBindings parses the annotation value into a slice of EdgeNumaNetworkBinding.
func ParseBindings(annotationValue string) ([]EdgeNumaNetworkBinding, error) {
	var bindings []EdgeNumaNetworkBinding
	if err := json.Unmarshal([]byte(annotationValue), &bindings); err != nil {
		return nil, fmt.Errorf("parse edge bindings: %w", err)
	}
	return bindings, nil
}

// validateBindings checks each binding for well-formedness and presence in the
// Pipeline's declared edges. It does NOT check numaNetwork existence (cluster
// access is required; see PipelineValidator.Handle).
func validateBindings(bindings []EdgeNumaNetworkBinding, edges []PipelineEdge) error {
	edgeSet := make(map[[2]string]struct{}, len(edges))
	for _, e := range edges {
		edgeSet[[2]string{e.From, e.To}] = struct{}{}
	}

	for i, b := range bindings {
		if b.From == "" || b.To == "" {
			return fmt.Errorf("binding[%d]: from and to must not be empty", i)
		}
		if b.ConnectionType != ConnectionTypeDirect {
			return fmt.Errorf("binding[%d]: connectionType %q is not supported (only %q is allowed; multi-isbsvc is reserved for a future milestone)",
				i, b.ConnectionType, ConnectionTypeDirect)
		}
		if _, ok := edgeSet[[2]string{b.From, b.To}]; !ok {
			return fmt.Errorf("binding[%d]: edge (%q -> %q) not found in Pipeline spec.edges", i, b.From, b.To)
		}
	}
	return nil
}
