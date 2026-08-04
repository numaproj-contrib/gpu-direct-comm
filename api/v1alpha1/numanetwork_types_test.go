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
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// User journey:
// As a Numaflow pipeline operator, I want to define a NumaNetwork resource
// that specifies a DRA DeviceClass and DRANET parameters (ipRange), so that
// GPU Direct communication can be configured declaratively.
// ethernetSpeed and vlanTag were removed in M2 (ADR-0002, ADR-0003).
//
// Spec reference: https://compsysg.atlassian.net/wiki/spaces/DCC/pages/1711996930

func TestNumaNetworkSpecFields(t *testing.T) {
	nn := NumaNetwork{
		Spec: NumaNetworkSpec{
			RefDeviceClass: RefDeviceClass{
				Name: "vf.nvidia.dra.net",
			},
			RefResourceClaimDranet: RefResourceClaimDranet{
				IPRange: "192.168.10.0/24",
			},
		},
		Status: NumaNetworkStatus{
			ResourceClaimTemplateName: "pipeline1-multi-network-rct",
		},
	}

	if nn.Spec.RefDeviceClass.Name != "vf.nvidia.dra.net" {
		t.Errorf("RefDeviceClass.Name = %q", nn.Spec.RefDeviceClass.Name)
	}
	if nn.Spec.RefResourceClaimDranet.IPRange != "192.168.10.0/24" {
		t.Errorf("IPRange = %q", nn.Spec.RefResourceClaimDranet.IPRange)
	}
	if nn.Status.ResourceClaimTemplateName != "pipeline1-multi-network-rct" {
		t.Errorf("ResourceClaimTemplateName = %q", nn.Status.ResourceClaimTemplateName)
	}
}

func TestNumaNetworkJSONTags(t *testing.T) {
	spec := NumaNetworkSpec{
		RefDeviceClass: RefDeviceClass{Name: "vf.nvidia.dra.net"},
		RefResourceClaimDranet: RefResourceClaimDranet{
			IPRange: "192.168.10.0/24",
		},
	}
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, key := range []string{
		`"refDeviceClass"`,
		`"name"`,
		`"refResourceClaimDranet"`,
		`"ipRange"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("spec JSON missing key %s: %s", key, s)
		}
	}
	// ethernetSpeed and vlanTag removed in M2 (ADR-0002, ADR-0003)
	for _, removed := range []string{`"ethernetSpeed"`, `"vlanTag"`} {
		if strings.Contains(s, removed) {
			t.Errorf("spec JSON must not contain removed field %s", removed)
		}
	}
}

// TestNumaNetworkDranetOnlyIPRange verifies that the JSON output contains only
// the ipRange field (ethernetSpeed and vlanTag were removed in M2 per ADR-0002/ADR-0003).
func TestNumaNetworkDranetOnlyIPRange(t *testing.T) {
	dranet := RefResourceClaimDranet{IPRange: "10.0.0.0/24"}
	b, err := json.Marshal(dranet)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"ipRange"`) {
		t.Errorf("ipRange missing: %s", s)
	}
	if strings.Contains(s, `"ethernetSpeed"`) {
		t.Errorf("ethernetSpeed must be absent (removed ADR-0002): %s", s)
	}
	if strings.Contains(s, `"vlanTag"`) {
		t.Errorf("vlanTag must be absent (removed ADR-0003): %s", s)
	}
}

// TestCRDManifest verifies the generated CRD YAML (make manifests) carries the
// kubebuilder markers required by the acceptance criteria.
func TestCRDManifest(t *testing.T) {
	b, err := os.ReadFile("../../config/crd/bases/numaflow.numaproj.io_numanetworks.yaml")
	if err != nil {
		t.Fatalf("CRD manifest not generated yet: %v", err)
	}
	var crd map[string]any
	if err := yaml.Unmarshal(b, &crd); err != nil {
		t.Fatalf("unmarshal CRD: %v", err)
	}

	// spec.names.shortNames contains "nn"
	names := dig(t, crd, "spec", "names")
	shortNames, _ := names["shortNames"].([]any)
	found := false
	for _, sn := range shortNames {
		if sn == "nn" {
			found = true
		}
	}
	if !found {
		t.Errorf("shortNames = %v, want to contain \"nn\"", shortNames)
	}

	versions, _ := dig(t, crd, "spec")["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}
	v0, ok := versions[0].(map[string]any)
	if !ok {
		t.Fatalf("versions[0] is not a map: %T", versions[0])
	}

	// subresources.status is defined
	sub, _ := v0["subresources"].(map[string]any)
	if _, ok := sub["status"]; !ok {
		t.Errorf("subresources.status missing: %v", sub)
	}

	// refDeviceClass and refResourceClaimDranet are both required in spec
	specSchema := dig(t, v0, "schema", "openAPIV3Schema", "properties", "spec")
	required, _ := specSchema["required"].([]any)
	wantRequired := []string{"refDeviceClass", "refResourceClaimDranet"}
	for _, want := range wantRequired {
		foundReq := false
		for _, r := range required {
			if r == want {
				foundReq = true
			}
		}
		if !foundReq {
			t.Errorf("spec.required = %v, want to contain %q", required, want)
		}
	}

	// refResourceClaimDranet.ipRange is required
	dranetSchema := dig(t, v0, "schema", "openAPIV3Schema", "properties", "spec",
		"properties", "refResourceClaimDranet")
	dranetRequired, _ := dranetSchema["required"].([]any)
	foundIPRange := false
	for _, r := range dranetRequired {
		if r == "ipRange" {
			foundIPRange = true
		}
	}
	if !foundIPRange {
		t.Errorf("refResourceClaimDranet.required = %v, want to contain \"ipRange\"", dranetRequired)
	}
}

// dig walks nested map[string]any keys, failing the test on a missing path.
func dig(t *testing.T, m map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := m
	for _, k := range path {
		next, ok := cur[k].(map[string]any)
		if !ok {
			t.Fatalf("path %v: key %q missing or not a map", path, k)
		}
		cur = next
	}
	return cur
}
