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
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// buildWebhookScheme builds a scheme containing types needed by the webhook tests.
func buildWebhookScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := numaflowv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("numaflowv1alpha1: %v", err)
	}
	return s
}

// newNumaNetwork returns a minimal NumaNetwork for seeding the fake client.
func newNumaNetwork(name string) *numaflowv1alpha1.NumaNetwork { //nolint:unparam
	return &numaflowv1alpha1.NumaNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: numaflowv1alpha1.NumaNetworkSpec{
			RefDeviceClass: numaflowv1alpha1.RefDeviceClass{Name: "vf.nvidia.dra.net"},
			RefResourceClaimDranet: numaflowv1alpha1.RefResourceClaimDranet{
				IPRange: "192.168.10.0/24",
			},
		},
	}
}

// pipelineJSON builds a minimal numaflow Pipeline JSON for the admission request.
func pipelineJSON(t *testing.T, ns string, annotations map[string]string, edges []map[string]string) []byte {
	t.Helper()
	obj := map[string]any{
		"apiVersion": "numaflow.numaproj.io/v1alpha1",
		"kind":       "Pipeline",
		"metadata": map[string]any{
			"name":        "test-pipeline",
			"namespace":   ns,
			"annotations": annotations,
		},
		"spec": map[string]any{
			"edges": func() []any {
				result := make([]any, 0, len(edges))
				for _, e := range edges {
					result = append(result, map[string]any{
						"from": e["from"],
						"to":   e["to"],
					})
				}
				return result
			}(),
		},
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal Pipeline: %v", err)
	}
	return raw
}

// makeRequest builds an admission.Request with the given Pipeline JSON.
func makeRequest(ns string, raw []byte) admission.Request {
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: ns,
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// validEdgeList is a convenience for tests.
var validEdgeList = []map[string]string{{"from": "filter-resize", "to": "inference"}}

func TestPipelineValidator_Handle(t *testing.T) {
	const ns = "default"
	const nnName = "pipeline1-multi-network"

	tests := []struct {
		name        string
		annotations map[string]string
		edges       []map[string]string
		seedNNs     []*numaflowv1alpha1.NumaNetwork
		wantAllowed bool
		// denyContains is checked only when wantAllowed == false
		denyContains string
	}{
		{
			name:        "(a) annotation absent → Allowed (non-breaking for existing Pipelines)",
			annotations: map[string]string{},
			edges:       validEdgeList,
			seedNNs:     nil,
			wantAllowed: true,
		},
		{
			name: "(b) valid binding + numaNetwork exists → Allowed",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"filter-resize","to":"inference","numaNetwork":"pipeline1-multi-network","connectionType":"direct"}]`,
			},
			edges:       validEdgeList,
			seedNNs:     []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed: true,
		},
		{
			name: "(c) JSON broken → Denied",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: "not-valid-json",
			},
			edges:        validEdgeList,
			seedNNs:      nil,
			wantAllowed:  false,
			denyContains: "parse edge bindings",
		},
		{
			name: "(d) connectionType multi-isbsvc → Denied",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"filter-resize","to":"inference","numaNetwork":"pipeline1-multi-network","connectionType":"multi-isbsvc"}]`,
			},
			edges:        validEdgeList,
			seedNNs:      nil,
			wantAllowed:  false,
			denyContains: "multi-isbsvc",
		},
		{
			name: "(e) edge not in spec.edges → Denied",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"a","to":"b","numaNetwork":"pipeline1-multi-network","connectionType":"direct"}]`,
			},
			edges:        validEdgeList,
			seedNNs:      nil,
			wantAllowed:  false,
			denyContains: "not found in Pipeline spec.edges",
		},
		{
			name: "(f) numaNetwork not found → Denied with name in message",
			annotations: map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"filter-resize","to":"inference","numaNetwork":"missing-nn","connectionType":"direct"}]`,
			},
			edges:        validEdgeList,
			seedNNs:      nil,
			wantAllowed:  false,
			denyContains: "missing-nn",
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
			validator := &PipelineValidator{Client: fakeClient, Scheme: s}

			raw := pipelineJSON(t, ns, tc.annotations, tc.edges)
			req := makeRequest(ns, raw)

			// Act
			resp := validator.Handle(context.Background(), req)

			// Assert
			if tc.wantAllowed {
				if !resp.Allowed {
					t.Errorf("expected Allowed, got Denied: %s", resp.Result.Message)
				}
			} else {
				if resp.Allowed {
					t.Errorf("expected Denied, got Allowed")
					return
				}
				if tc.denyContains != "" && !strings.Contains(resp.Result.Message, tc.denyContains) {
					t.Errorf("deny message %q does not contain %q", resp.Result.Message, tc.denyContains)
				}
			}
		})
	}
}
