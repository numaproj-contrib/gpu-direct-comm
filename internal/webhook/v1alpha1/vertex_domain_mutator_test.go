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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// vertexPodJSON builds a minimal Vertex Pod JSON for admission request testing.
func vertexPodJSON(t *testing.T, ns string, labels map[string]string) []byte {
	t.Helper()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: ns,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main", Image: "busybox"},
			},
		},
	}
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal Pod: %v", err)
	}
	return raw
}

// makePodRequest builds an admission.Request for a Pod CREATE.
func makePodRequest(ns string, raw []byte) admission.Request {
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: ns,
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// pipelineUnstructured builds an unstructured Pipeline object for seeding the fake client.
func pipelineUnstructured(t *testing.T, ns, name string, annotations map[string]string) *unstructured.Unstructured { //nolint:unparam
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("numaflow.numaproj.io/v1alpha1")
	obj.SetKind("Pipeline")
	obj.SetNamespace(ns)
	obj.SetName(name)
	obj.SetAnnotations(annotations)
	return obj
}

// extractPodLabel unmarshals the (potentially patched) Pod JSON and returns a label value.
func extractPodLabel(t *testing.T, podJSON []byte, labelKey string) string {
	t.Helper()
	var pod corev1.Pod
	if err := json.Unmarshal(podJSON, &pod); err != nil {
		t.Fatalf("unmarshal Pod: %v", err)
	}
	return pod.Labels[labelKey]
}

func TestVertexDomainMutator_Handle(t *testing.T) {
	const ns = "default"
	const pipelineName = "e2e-gpu-direct-pipeline"
	const vertexName = "in"
	const nnName = "e2e-numanetwork"

	directBinding := `[{"from":"in","to":"out","numaNetwork":"` + nnName + `","connectionType":"direct"}]`

	vertexLabels := map[string]string{
		"numaflow.numaproj.io/pipeline-name": pipelineName,
		"numaflow.numaproj.io/vertex-name":   vertexName,
	}

	tests := []struct {
		name string
		// labels on the Pod
		podLabels map[string]string
		// Pipeline object to seed in the fake client (nil = not present)
		pipeline *unstructured.Unstructured
		// seed NumaNetworks (for scheme registration)
		seedNNs []*numaflowv1alpha1.NumaNetwork

		wantAllowed bool
		// wantLabel is the expected vertex-domain label value (empty if not expecting a patch)
		wantLabel string
		// wantNoPatches is true when no patches are expected
		wantNoPatches bool
		// denyContains is checked only when wantAllowed == false
		denyContains string
	}{
		{
			name:      "(a) non-vertex Pod (no numaflow labels) → Allowed, no patches",
			podLabels: map[string]string{"app": "nginx"},
			pipeline: pipelineUnstructured(t, ns, pipelineName, map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			}),
			wantAllowed:   true,
			wantNoPatches: true,
		},
		{
			name: "(b) vertex Pod missing vertex-name label → Allowed, no patches",
			podLabels: map[string]string{
				"numaflow.numaproj.io/pipeline-name": pipelineName,
			},
			pipeline: pipelineUnstructured(t, ns, pipelineName, map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			}),
			wantAllowed:   true,
			wantNoPatches: true,
		},
		{
			name:      "(c) vertex Pod, Pipeline has direct binding → Allowed, vertex-domain label injected",
			podLabels: vertexLabels,
			pipeline: pipelineUnstructured(t, ns, pipelineName, map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			}),
			seedNNs:     []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed: true,
			wantLabel:   "in.e2e-gpu-direct-pipeline.default.vertexdomain.local",
		},
		{
			name:          "(d) vertex Pod, Pipeline without numa-network-edges annotation → Allowed, no patches",
			podLabels:     vertexLabels,
			pipeline:      pipelineUnstructured(t, ns, pipelineName, map[string]string{}),
			seedNNs:       nil,
			wantAllowed:   true,
			wantNoPatches: true,
		},
		{
			name:          "(e) vertex Pod, Pipeline not found → Allowed, no patches (non-breaking)",
			podLabels:     vertexLabels,
			pipeline:      nil,
			seedNNs:       nil,
			wantAllowed:   true,
			wantNoPatches: true,
		},
		{
			name: "(f) vertex Pod, vertex not in direct bindings → Allowed, no patches",
			podLabels: map[string]string{
				"numaflow.numaproj.io/pipeline-name": pipelineName,
				"numaflow.numaproj.io/vertex-name":   "unrelated-vertex",
			},
			pipeline: pipelineUnstructured(t, ns, pipelineName, map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			}),
			seedNNs:       []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed:   true,
			wantNoPatches: true,
		},
		{
			name: "(g) vertex Pod, both from and to get labels → verify 'out' vertex",
			podLabels: map[string]string{
				"numaflow.numaproj.io/pipeline-name": pipelineName,
				"numaflow.numaproj.io/vertex-name":   "out",
			},
			pipeline: pipelineUnstructured(t, ns, pipelineName, map[string]string{
				AnnotationNumaNetworkEdges: directBinding,
			}),
			seedNNs:     []*numaflowv1alpha1.NumaNetwork{newNumaNetwork(nnName)},
			wantAllowed: true,
			wantLabel:   "out.e2e-gpu-direct-pipeline.default.vertexdomain.local",
		},
		{
			name:      "(h) vertex Pod, only non-direct bindings → Allowed, no patches",
			podLabels: vertexLabels,
			pipeline: pipelineUnstructured(t, ns, pipelineName, map[string]string{
				AnnotationNumaNetworkEdges: `[{"from":"in","to":"out","numaNetwork":"` + nnName + `","connectionType":"multi-isbsvc"}]`,
			}),
			seedNNs:       nil,
			wantAllowed:   true,
			wantNoPatches: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			s := buildWebhookScheme(t)
			objects := make([]runtime.Object, 0)
			for _, nn := range tc.seedNNs {
				objects = append(objects, nn)
			}
			if tc.pipeline != nil {
				objects = append(objects, tc.pipeline)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objects...).Build()
			mutator := &VertexDomainMutator{Client: fakeClient, Scheme: s}

			raw := vertexPodJSON(t, ns, tc.podLabels)
			req := makePodRequest(ns, raw)

			// Act
			resp := mutator.Handle(context.Background(), req)

			// Assert
			if tc.wantAllowed && !resp.Allowed {
				t.Fatalf("expected Allowed, got Denied: %s", resp.Result.Message)
			}
			if !tc.wantAllowed && resp.Allowed {
				t.Fatal("expected Denied, got Allowed")
			}
			if !tc.wantAllowed && tc.denyContains != "" {
				if !strings.Contains(resp.Result.Message, tc.denyContains) {
					t.Errorf("deny message %q does not contain %q", resp.Result.Message, tc.denyContains)
				}
				return
			}

			if tc.wantNoPatches {
				if len(resp.Patches) > 0 {
					t.Errorf("expected no patches, got %d: %v", len(resp.Patches), resp.Patches)
				}
				return
			}

			// Apply patches and verify label
			patched := applyPatches(t, raw, resp.Patches)
			gotLabel := extractPodLabel(t, patched, LabelVertexDomain)
			if gotLabel != tc.wantLabel {
				t.Errorf("vertex-domain label: got %q, want %q", gotLabel, tc.wantLabel)
			}
		})
	}
}
