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
// specifying a DRA DeviceClass and connection type, so that GPU Direct
// communication can be configured declaratively for pipelines.

func TestConnectionTypeConstants(t *testing.T) {
	if string(ConnectionTypeDirect) != "direct" {
		t.Errorf("ConnectionTypeDirect = %q, want %q", ConnectionTypeDirect, "direct")
	}
	if string(ConnectionTypeMultiISBSvc) != "multi-isbsvc" {
		t.Errorf("ConnectionTypeMultiISBSvc = %q, want %q", ConnectionTypeMultiISBSvc, "multi-isbsvc")
	}
}

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

func TestNumaNetworkSpecAndStatusFields(t *testing.T) {
	nn := NumaNetwork{
		Spec: NumaNetworkSpec{
			DeviceClassName: "gpu.nvidia.com",
			ConnectionType:  ConnectionTypeDirect,
		},
		Status: NumaNetworkStatus{
			Phase:                     NumaNetworkPhasePending,
			Conditions:                []metav1.Condition{},
			ResourceClaimTemplateName: "nn-rct",
		},
	}

	if nn.Spec.DeviceClassName != "gpu.nvidia.com" {
		t.Errorf("DeviceClassName = %q", nn.Spec.DeviceClassName)
	}
	if nn.Status.ResourceClaimTemplateName != "nn-rct" {
		t.Errorf("ResourceClaimTemplateName = %q", nn.Status.ResourceClaimTemplateName)
	}
}

func TestNumaNetworkJSONTags(t *testing.T) {
	nn := NumaNetwork{
		Spec: NumaNetworkSpec{
			DeviceClassName: "gpu.nvidia.com",
			ConnectionType:  ConnectionTypeDirect,
		},
	}
	b, err := json.Marshal(nn.Spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, key := range []string{`"deviceClassName"`, `"connectionType"`} {
		if !strings.Contains(s, key) {
			t.Errorf("spec JSON %s missing key %s", s, key)
		}
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

	// spec.connectionType has default "direct"
	connType := dig(t, v0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "connectionType")
	if connType["default"] != "direct" {
		t.Errorf("connectionType default = %v, want \"direct\"", connType["default"])
	}

	// deviceClassName is required
	specSchema := dig(t, v0, "schema", "openAPIV3Schema", "properties", "spec")
	required, _ := specSchema["required"].([]interface{})
	foundReq := false
	for _, r := range required {
		if r == "deviceClassName" {
			foundReq = true
		}
	}
	if !foundReq {
		t.Errorf("spec.required = %v, want to contain \"deviceClassName\"", required)
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
