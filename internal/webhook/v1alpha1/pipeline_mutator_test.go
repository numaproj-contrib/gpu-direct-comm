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
	"context"
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	gomodulesjsonpatch "gomodules.xyz/jsonpatch/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// pipelineWithVerticesJSON builds a Pipeline JSON that includes spec.vertices in addition to spec.edges.
func pipelineWithVerticesJSON(t *testing.T, ns string, annotations map[string]string, vertices []map[string]any, edges []map[string]string) []byte {
	t.Helper()
	rawEdges := make([]any, 0, len(edges))
	for _, e := range edges {
		rawEdges = append(rawEdges, map[string]any{"from": e["from"], "to": e["to"]})
	}
	obj := map[string]any{
		"apiVersion": "numaflow.numaproj.io/v1alpha1",
		"kind":       "Pipeline",
		"metadata": map[string]any{
			"name":        "test-pipeline",
			"namespace":   ns,
			"annotations": annotations,
		},
		"spec": map[string]any{
			"edges":    rawEdges,
			"vertices": vertices,
		},
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal Pipeline: %v", err)
	}
	return raw
}

// applyPatches applies JSON Patch operations (from admission.Response.Patches) to the original JSON.
// It converts gomodules.xyz/jsonpatch/v2 operations to evanphx/json-patch/v5 by marshaling through JSON.
func applyPatches(t *testing.T, original []byte, patches []gomodulesjsonpatch.JsonPatchOperation) []byte {
	t.Helper()
	if len(patches) == 0 {
		return original
	}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		t.Fatalf("marshal patches: %v", err)
	}
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	result, err := patch.Apply(original)
	if err != nil {
		t.Fatalf("apply patch: %v", err)
	}
	return result
}

// resourceClaimsForVertex extracts spec.vertices[i].resourceClaims from a Pipeline JSON.
func resourceClaimsForVertex(t *testing.T, pipelineJSON []byte, vertexName string) []map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(pipelineJSON, &obj); err != nil {
		t.Fatalf("unmarshal pipeline: %v", err)
	}
	spec, _ := obj["spec"].(map[string]any)
	vertices, _ := spec["vertices"].([]any)
	for _, v := range vertices {
		vertex, _ := v.(map[string]any)
		if vertex["name"] == vertexName {
			rawClaims, _ := vertex["resourceClaims"].([]any)
			claims := make([]map[string]any, 0, len(rawClaims))
			for _, c := range rawClaims {
				if m, ok := c.(map[string]any); ok {
					claims = append(claims, m)
				}
			}
			return claims
		}
	}
	return nil
}

func TestPipelineMutator_Handle(t *testing.T) {
	const ns = "default"
	const nnName = "pipeline1-multi-network"
	const rctName = nnName + "-rct"

	baseVertices := []map[string]any{
		{"name": "filter-resize"},
		{"name": "inference"},
	}
	baseEdges := []map[string]string{{"from": "filter-resize", "to": "inference"}}
	directBinding := `[{"from":"filter-resize","to":"inference","numaNetwork":"pipeline1-multi-network","connectionType":"direct"}]`

	tests := []struct {
		name string
		// annotations on the Pipeline
		annotations map[string]string
		// vertices in spec.vertices
		vertices []map[string]any
		edges    []map[string]string
		seedNNs  []*numaflowv1alpha1.NumaNetwork

		wantAllowed bool
		// wantPatches is false when we expect no patches (annotation absent or no direct bindings)
		wantPatches bool
		// vertexClaims maps vertex name → expected resourceClaims entries (checked only when wantPatches)
		vertexClaims map[string][]map[string]any
	}{
		{
			name:        "(a) annotation absent → Allowed, no patches",
			annotations: map[string]string{},
			vertices:    baseVertices,
			edges:       baseEdges,
			seedNNs:     nil,
			wantAllowed: true,
			wantPatches: false,
		},
		{
			name: "(b) valid direct binding → Allowed, from and to both get resourceClaims patch",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			},
			vertices:    baseVertices,
			edges:       baseEdges,
			seedNNs:     []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed: true,
			wantPatches: true,
			vertexClaims: map[string][]map[string]any{
				"filter-resize": {{"name": rctName, "resourceClaimTemplateName": rctName}},
				"inference":     {{"name": rctName, "resourceClaimTemplateName": rctName}},
			},
		},
		{
			name: "(c) multiple bindings sharing one vertex → no duplicates in resourceClaims",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"a","to":"b","numaNetwork":"pipeline1-multi-network","connectionType":"direct"},{"from":"b","to":"c","numaNetwork":"pipeline1-multi-network","connectionType":"direct"}]`,
			},
			vertices: []map[string]any{
				{"name": "a"},
				{"name": "b"},
				{"name": "c"},
			},
			edges: []map[string]string{
				{"from": "a", "to": "b"},
				{"from": "b", "to": "c"},
			},
			seedNNs:     []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed: true,
			wantPatches: true,
			vertexClaims: map[string][]map[string]any{
				// vertex "b" is shared by two bindings, but should appear only once
				"b": {{"name": rctName, "resourceClaimTemplateName": rctName}},
			},
		},
		{
			name: "(d) vertex already has resourceClaims → injected entry appended, existing preserved",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			},
			vertices: []map[string]any{
				{
					"name": "filter-resize",
					"resourceClaims": []any{
						map[string]any{"name": "existing-claim", "resourceClaimTemplateName": "existing-rct"},
					},
				},
				{"name": "inference"},
			},
			edges:       baseEdges,
			seedNNs:     []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed: true,
			wantPatches: true,
			vertexClaims: map[string][]map[string]any{
				"filter-resize": {
					{"name": "existing-claim", "resourceClaimTemplateName": "existing-rct"},
					{"name": rctName, "resourceClaimTemplateName": rctName},
				},
			},
		},
		{
			name: "(e) connectionType not direct → binding skipped, no patches",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"filter-resize","to":"inference","numaNetwork":"pipeline1-multi-network","connectionType":"multi-isbsvc"}]`,
			},
			vertices:    baseVertices,
			edges:       baseEdges,
			seedNNs:     nil,
			wantAllowed: true,
			wantPatches: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			s := buildWebhookScheme(t)
			objects := make([]runtime.Object, 0, len(tc.seedNNs))
			for _, nn := range tc.seedNNs {
				objects = append(objects, nn)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objects...).Build()
			mutator := &PipelineMutator{Client: fakeClient, Scheme: s}

			raw := pipelineWithVerticesJSON(t, ns, tc.annotations, tc.vertices, tc.edges)
			req := makeRequest(ns, raw)

			// Act
			resp := mutator.Handle(context.Background(), req)

			// Assert
			if tc.wantAllowed && !resp.Allowed {
				t.Fatalf("expected Allowed, got Denied: %s", resp.Result.Message)
			}
			if !tc.wantAllowed && resp.Allowed {
				t.Fatal("expected Denied, got Allowed")
			}

			if !tc.wantPatches {
				if len(resp.Patches) > 0 {
					t.Errorf("expected no patches, got %d: %v", len(resp.Patches), resp.Patches)
				}
				return
			}

			// Apply patches and validate resourceClaims per vertex
			patched := applyPatches(t, raw, resp.Patches)
			for vertexName, expectedClaims := range tc.vertexClaims {
				gotClaims := resourceClaimsForVertex(t, patched, vertexName)
				if len(gotClaims) != len(expectedClaims) {
					t.Errorf("vertex %q: got %d resourceClaims, want %d: %v", vertexName, len(gotClaims), len(expectedClaims), gotClaims)
					continue
				}
				for i, want := range expectedClaims {
					got := gotClaims[i]
					for k, wantV := range want {
						if got[k] != wantV {
							t.Errorf("vertex %q resourceClaims[%d][%q]: got %q, want %q", vertexName, i, k, got[k], wantV)
						}
					}
				}
			}
		})
	}
}
