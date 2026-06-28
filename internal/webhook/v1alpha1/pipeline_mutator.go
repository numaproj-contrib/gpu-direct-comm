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
func (m *PipelineMutator) Handle(_ context.Context, _ admission.Request) admission.Response {
	return admission.Allowed("not implemented")
}
