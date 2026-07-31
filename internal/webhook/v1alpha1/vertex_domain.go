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
	"fmt"
	"regexp"
)

const (
	// LabelVertexDomain is the label key for the vertexDomain FQDN injected
	// into Vertex Pods by the VertexDomainMutator webhook.
	LabelVertexDomain = "gpu-direct-comm.numaproj.io/vertex-domain"

	// vertexDomainSuffix is the DNS zone suffix appended to every vertexDomain FQDN.
	vertexDomainSuffix = "vertexdomain.local"

	// maxLabelValueLength is the Kubernetes label value length limit.
	maxLabelValueLength = 63
)

// dnsLabelRegexp matches a valid DNS label: lowercase alphanumeric, hyphens allowed
// in the middle, no leading/trailing hyphens.
var dnsLabelRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// BuildVertexDomain computes the vertexDomain FQDN for a given vertex.
// Format: <vertex>.<pipeline>.<namespace>.vertexdomain.local
func BuildVertexDomain(vertex, pipeline, namespace string) (string, error) {
	if vertex == "" {
		return "", fmt.Errorf("vertex name must not be empty")
	}
	if pipeline == "" {
		return "", fmt.Errorf("pipeline name must not be empty")
	}
	if namespace == "" {
		return "", fmt.Errorf("namespace must not be empty")
	}

	for _, part := range []struct{ name, value string }{
		{"vertex", vertex},
		{"pipeline", pipeline},
		{"namespace", namespace},
	} {
		if !dnsLabelRegexp.MatchString(part.value) {
			return "", fmt.Errorf("%s %q contains non-DNS-compatible characters", part.name, part.value)
		}
	}

	fqdn := fmt.Sprintf("%s.%s.%s.%s", vertex, pipeline, namespace, vertexDomainSuffix)

	if len(fqdn) > maxLabelValueLength {
		return "", fmt.Errorf("vertexDomain FQDN %q exceeds 63 character label value limit (%d chars)", fqdn, len(fqdn))
	}

	return fqdn, nil
}
