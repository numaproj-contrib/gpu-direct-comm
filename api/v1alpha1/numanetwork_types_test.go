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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// User journey:
// As a Numaflow pipeline operator, I want to define a NumaNetwork resource
// that specifies a DRA DeviceClass and DRANET parameters (ipRange, ethernetSpeed,
// vlanTag), so that GPU Direct communication can be configured declaratively.
//
// Spec reference: https://compsysg.atlassian.net/wiki/spaces/DCC/pages/1711996930

func TestNumaNetworkPhaseConstants(t *testing.T) {
	cases := []struct {
		got  NumaNetworkPhase
		want string
	}{
		{NumaNetworkPhaseUnknown, ""},
		{NumaNetworkPhasePending, "Pending"},
		{NumaNetworkPhaseRunning, "Running"},
		{NumaNetworkPhaseFailed, "Failed"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("phase constant = %q, want %q", c.got, c.want)
		}
	}
}

// TestConnectionTypeConstants verifies the constants used in Pipeline edges.
// connectionType is NOT part of NumaNetworkSpec; it appears in
// Pipeline.spec.edges[].numaNetwork.connectionType.
func TestConnectionTypeConstants(t *testing.T) {
	if string(ConnectionTypeDirect) != "direct" {
		t.Errorf("ConnectionTypeDirect = %q, want %q", ConnectionTypeDirect, "direct")
	}
	if string(ConnectionTypeMultiISBSvc) != "multi-isbsvc" {
		t.Errorf("ConnectionTypeMultiISBSvc = %q, want %q", ConnectionTypeMultiISBSvc, "multi-isbsvc")
	}
}

func TestNumaNetworkSpecFields(t *testing.T) {
	nn := NumaNetwork{
		Spec: NumaNetworkSpec{
			RefDeviceClass: RefDeviceClass{
				Name: "vf.nvidia.dra.net",
			},
			RefResourceClaimDranet: RefResourceClaimDranet{
				IPRange:       "192.168.10.0/24",
				EthernetSpeed: 100,
				VlanTag:       10,
			},
		},
		Status: NumaNetworkStatus{
			Phase:                     NumaNetworkPhasePending,
			Conditions:                []metav1.Condition{},
			ResourceClaimTemplateName: "pipeline1-multi-network-rct",
		},
	}

	if nn.Spec.RefDeviceClass.Name != "vf.nvidia.dra.net" {
		t.Errorf("RefDeviceClass.Name = %q", nn.Spec.RefDeviceClass.Name)
	}
	if nn.Spec.RefResourceClaimDranet.IPRange != "192.168.10.0/24" {
		t.Errorf("IPRange = %q", nn.Spec.RefResourceClaimDranet.IPRange)
	}
	if nn.Spec.RefResourceClaimDranet.EthernetSpeed != 100 {
		t.Errorf("EthernetSpeed = %d", nn.Spec.RefResourceClaimDranet.EthernetSpeed)
	}
	if nn.Spec.RefResourceClaimDranet.VlanTag != 10 {
		t.Errorf("VlanTag = %d", nn.Spec.RefResourceClaimDranet.VlanTag)
	}
	if nn.Status.ResourceClaimTemplateName != "pipeline1-multi-network-rct" {
		t.Errorf("ResourceClaimTemplateName = %q", nn.Status.ResourceClaimTemplateName)
	}
}

// TestNumaNetworkSpecOptionalDranetFields verifies that ethernetSpeed and
// vlanTag are optional — a spec with only ipRange must be valid.
func TestNumaNetworkSpecOptionalDranetFields(t *testing.T) {
	nn := NumaNetwork{
		Spec: NumaNetworkSpec{
			RefDeviceClass:         RefDeviceClass{Name: "vf.nvidia.dra.net"},
			RefResourceClaimDranet: RefResourceClaimDranet{IPRange: "10.0.0.0/24"},
		},
	}
	if nn.Spec.RefResourceClaimDranet.EthernetSpeed != 0 {
		t.Errorf("EthernetSpeed zero value expected, got %d", nn.Spec.RefResourceClaimDranet.EthernetSpeed)
	}
	if nn.Spec.RefResourceClaimDranet.VlanTag != 0 {
		t.Errorf("VlanTag zero value expected, got %d", nn.Spec.RefResourceClaimDranet.VlanTag)
	}
}

func TestNumaNetworkJSONTags(t *testing.T) {
	spec := NumaNetworkSpec{
		RefDeviceClass: RefDeviceClass{Name: "vf.nvidia.dra.net"},
		RefResourceClaimDranet: RefResourceClaimDranet{
			IPRange:       "192.168.10.0/24",
			EthernetSpeed: 100,
			VlanTag:       10,
		},
	}
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, key := range []string{
		`"refDeviceClass"`,
		`"refResourceClaimDranet"`,
		`"ipRange"`,
		`"ethernetSpeed"`,
		`"vlanTag"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("spec JSON missing key %s: %s", key, s)
		}
	}
}

// TestNumaNetworkDranetOmitempty verifies that zero-value optional fields are
// omitted from JSON so generated ResourceClaimTemplate manifests stay clean.
func TestNumaNetworkDranetOmitempty(t *testing.T) {
	spec := NumaNetworkSpec{
		RefDeviceClass:         RefDeviceClass{Name: "vf.nvidia.dra.net"},
		RefResourceClaimDranet: RefResourceClaimDranet{IPRange: "10.0.0.0/24"},
	}
	b, err := json.Marshal(spec.RefResourceClaimDranet)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"ethernetSpeed"`) {
		t.Errorf("zero ethernetSpeed should be omitted: %s", s)
	}
	if strings.Contains(s, `"vlanTag"`) {
		t.Errorf("zero vlanTag should be omitted: %s", s)
	}
}

// TestCRDManifest verifies the generated CRD YAML (make manifests) carries the
// kubebuilder markers required by the acceptance criteria.
func TestCRDManifest(t *testing.T) {
	b, err := os.ReadFile("../../config/crd/bases/numaflow.numaproj.io_numanetworks.yaml")
	if err != nil {
		t.Fatalf("CRD manifest not generated yet: %v", err)
	}
	var crd map[string]interface{}
	if err := yaml.Unmarshal(b, &crd); err != nil {
		t.Fatalf("unmarshal CRD: %v", err)
	}

	// spec.names.shortNames contains "nn"
	names := dig(t, crd, "spec", "names")
	shortNames, _ := names["shortNames"].([]interface{})
	found := false
	for _, sn := range shortNames {
		if sn == "nn" {
			found = true
		}
	}
	if !found {
		t.Errorf("shortNames = %v, want to contain \"nn\"", shortNames)
	}

	versions, _ := dig(t, crd, "spec")["versions"].([]interface{})
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}
	v0 := versions[0].(map[string]interface{})

	// subresources.status is defined
	sub, _ := v0["subresources"].(map[string]interface{})
	if _, ok := sub["status"]; !ok {
		t.Errorf("subresources.status missing: %v", sub)
	}

	// refDeviceClass and refResourceClaimDranet are both required in spec
	specSchema := dig(t, v0, "schema", "openAPIV3Schema", "properties", "spec")
	required, _ := specSchema["required"].([]interface{})
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
	dranetRequired, _ := dranetSchema["required"].([]interface{})
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

// dig walks nested map[string]interface{} keys, failing the test on a missing path.
func dig(t *testing.T, m map[string]interface{}, path ...string) map[string]interface{} {
	t.Helper()
	cur := m
	for _, k := range path {
		next, ok := cur[k].(map[string]interface{})
		if !ok {
			t.Fatalf("path %v: key %q missing or not a map", path, k)
		}
		cur = next
	}
	return cur
}
