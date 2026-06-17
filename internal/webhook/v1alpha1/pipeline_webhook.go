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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// +kubebuilder:webhook:path=/validate-numaflow-numaproj-io-v1alpha1-pipeline,mutating=false,failurePolicy=fail,sideEffects=None,groups=numaflow.numaproj.io,resources=pipelines,verbs=create;update,versions=v1alpha1,name=vpipeline.numaproj.io,admissionReviewVersions=v1
// +kubebuilder:rbac:groups=numaflow.numaproj.io,resources=numanetworks,verbs=get;list;watch

// PipelineValidator validates numaflow Pipeline admission requests.
// It decodes the Pipeline as unstructured to avoid importing numaflow as a Go dependency (ADR-0005).
type PipelineValidator struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// Handle implements admission.Handler.
func (v *PipelineValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.Object.Raw, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode Pipeline: %w", err))
	}

	annotations := obj.GetAnnotations()
	rawBinding, ok := annotations[AnnotationNumaNetworkEdges]
	if !ok {
		// No binding annotation — allow as-is to preserve non-breaking behaviour
		// for existing Pipelines that do not use gpu-direct-comm.
		return admission.Allowed("no numa-network-edges annotation")
	}

	bindings, err := ParseBindings(rawBinding)
	if err != nil {
		return admission.Denied(err.Error())
	}

	edges := extractEdges(obj)

	if err := validateBindings(bindings, edges); err != nil {
		return admission.Denied(err.Error())
	}

	ns := obj.GetNamespace()
	for _, b := range bindings {
		nn := &numaflowv1alpha1.NumaNetwork{}
		if err := v.Client.Get(ctx, types.NamespacedName{Namespace: ns, Name: b.NumaNetwork}, nn); err != nil {
			if apierrors.IsNotFound(err) {
				return admission.Denied(fmt.Sprintf("numaNetwork %q not found in namespace %q", b.NumaNetwork, ns))
			}
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("get NumaNetwork %q: %w", b.NumaNetwork, err))
		}
	}

	return admission.Allowed("")
}

// extractEdges reads spec.edges from an unstructured Pipeline object.
// Returns an empty slice when spec or edges is absent.
func extractEdges(obj *unstructured.Unstructured) []PipelineEdge {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return nil
	}
	rawEdges, ok := spec["edges"].([]any)
	if !ok {
		return nil
	}
	edges := make([]PipelineEdge, 0, len(rawEdges))
	for _, e := range rawEdges {
		edge, ok := e.(map[string]any)
		if !ok {
			continue
		}
		from, _ := edge["from"].(string)
		to, _ := edge["to"].(string)
		edges = append(edges, PipelineEdge{From: from, To: to})
	}
	return edges
}
