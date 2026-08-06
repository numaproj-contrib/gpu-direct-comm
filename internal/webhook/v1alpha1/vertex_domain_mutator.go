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
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// numaflowLabelPipelineName is the upstream Numaflow label key set on Vertex Pods.
	numaflowLabelPipelineName = "numaflow.numaproj.io/pipeline-name"

	// numaflowLabelVertexName is the upstream Numaflow label key set on Vertex Pods.
	numaflowLabelVertexName = "numaflow.numaproj.io/vertex-name"
)

// +kubebuilder:rbac:groups=numaflow.numaproj.io,resources=pipelines,verbs=get;list;watch
// +kubebuilder:webhook:path=/mutate-v1-pod-vertex-domain,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mvertexdomain.numaproj.io,admissionReviewVersions=v1

// VertexDomainMutator injects a vertexDomain marker label and FQDN annotation
// into Vertex Pods that participate in a numaNetwork direct binding.
type VertexDomainMutator struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// Handle implements admission.Handler for Pod CREATE requests.
func (m *VertexDomainMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx).WithName("vertex-domain-mutator")

	pod := &corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode Pod: %w", err))
	}

	pipelineName := pod.Labels[numaflowLabelPipelineName]
	vertexName := pod.Labels[numaflowLabelVertexName]
	if pipelineName == "" || vertexName == "" {
		return admission.Allowed("not a Numaflow Vertex Pod")
	}

	ns := req.Namespace
	if ns == "" {
		ns = pod.Namespace
	}

	pipeline := &unstructured.Unstructured{}
	pipeline.SetAPIVersion("numaflow.numaproj.io/v1alpha1")
	pipeline.SetKind("Pipeline")
	if err := m.Client.Get(ctx, types.NamespacedName{Namespace: ns, Name: pipelineName}, pipeline); err != nil {
		log.V(1).Info("Pipeline not found, skipping vertex-domain injection", "pipeline", pipelineName, "error", err)
		return admission.Allowed("Pipeline not found, skipping")
	}

	annotations := pipeline.GetAnnotations()
	rawBinding, ok := annotations[AnnotationNumaNetworkEdges]
	if !ok {
		return admission.Allowed("no numa-network-edges annotation on Pipeline")
	}

	bindings, err := ParseBindings(rawBinding)
	if err != nil {
		log.V(1).Info("Failed to parse bindings, skipping", "error", err)
		return admission.Allowed("binding parse error, skipping")
	}

	if !vertexInDirectBindings(vertexName, bindings) {
		return admission.Allowed("vertex not in direct bindings")
	}

	fqdn, err := BuildVertexDomain(vertexName, pipelineName, ns)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("build vertexDomain: %w", err))
	}

	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels[LabelVertexDomain] = LabelVertexDomainValue

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[AnnotationVertexDomainFQDN] = fqdn

	patched, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("marshal patched Pod: %w", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, patched)
}

// vertexInDirectBindings returns true if the vertex participates in at least
// one direct-type binding (as either from or to).
func vertexInDirectBindings(vertex string, bindings []EdgeNumaNetworkBinding) bool {
	for _, b := range bindings {
		if b.ConnectionType != ConnectionTypeDirect {
			continue
		}
		if b.From == vertex || b.To == vertex {
			return true
		}
	}
	return false
}
