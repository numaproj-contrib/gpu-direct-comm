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

// Package v1alpha1 provides admission webhook handlers for gpu-direct-comm.
package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/mutate-numaflow-numaproj-io-v1alpha1-pipeline,mutating=true,failurePolicy=fail,sideEffects=None,groups=numaflow.numaproj.io,resources=pipelines,verbs=create;update,versions=v1alpha1,name=mpipeline.numaproj.io,admissionReviewVersions=v1

// PipelineMutator injects ResourceClaim references into Pipeline Vertex templates
// based on edge-to-numaNetwork bindings declared in the Pipeline annotation (ADR-0009).
type PipelineMutator struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// Handle implements admission.Handler.
func (m *PipelineMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.Object.Raw, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode Pipeline: %w", err))
	}

	annotations := obj.GetAnnotations()
	rawBinding, ok := annotations[AnnotationNumaNetworkEdges]
	if !ok {
		return admission.Allowed("no numa-network-edges annotation")
	}

	bindings, err := ParseBindings(rawBinding)
	if err != nil {
		// Let the Validating Webhook surface the parse error; Mutating side just allows.
		return admission.Allowed("parse error deferred to validating webhook")
	}

	// Collect vertices that need RCT injection (deduplication by vertex+RCT name).
	// vertex name → ordered slice of RCT names to add.
	toInject := map[string][]string{}
	queued := map[string]map[string]struct{}{} // vertex → set of already-queued RCT names

	for _, b := range bindings {
		if b.ConnectionType != ConnectionTypeDirect {
			continue
		}
		rctName := b.NumaNetwork + "-rct"
		for _, vertexName := range []string{b.From, b.To} {
			if _, exists := queued[vertexName]; !exists {
				queued[vertexName] = map[string]struct{}{}
			}
			if _, dup := queued[vertexName][rctName]; dup {
				continue
			}
			queued[vertexName][rctName] = struct{}{}
			toInject[vertexName] = append(toInject[vertexName], rctName)
		}
	}

	if len(toInject) == 0 {
		return admission.Allowed("no direct bindings to inject")
	}

	// Mutate spec.vertices in the unstructured object.
	if err := injectResourceClaims(obj, toInject); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	patched, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("marshal patched Pipeline: %w", err))
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, patched)
}

// injectResourceClaims writes the given RCT names into spec.vertices[*].resourceClaims
// for each named vertex. Existing entries are preserved; duplicates are skipped.
func injectResourceClaims(obj *unstructured.Unstructured, toInject map[string][]string) error {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return fmt.Errorf("spec is not a map")
	}
	rawVertices, _ := spec["vertices"].([]any)

	for i, v := range rawVertices {
		vertex, ok := v.(map[string]any)
		if !ok {
			continue
		}
		vertexName, _ := vertex["name"].(string)
		rctNames, ok := toInject[vertexName]
		if !ok {
			continue
		}

		existing, _ := vertex["resourceClaims"].([]any)
		existingNames := make(map[string]struct{}, len(existing))
		for _, c := range existing {
			if m, ok := c.(map[string]any); ok {
				if n, ok := m["name"].(string); ok {
					existingNames[n] = struct{}{}
				}
			}
		}

		claims := existing
		for _, rctName := range rctNames {
			if _, dup := existingNames[rctName]; dup {
				continue
			}
			claims = append(claims, map[string]any{
				"name":                      rctName,
				"resourceClaimTemplateName": rctName,
			})
		}
		vertex["resourceClaims"] = claims
		rawVertices[i] = vertex
	}
	spec["vertices"] = rawVertices
	return nil
}
