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
	"strings"
	"testing"
)

func TestVertexDomainConstants(t *testing.T) {
	if LabelVertexDomainValue != "true" {
		t.Errorf("LabelVertexDomainValue = %q, want %q", LabelVertexDomainValue, "true")
	}
	if AnnotationVertexDomainFQDN == "" {
		t.Error("AnnotationVertexDomainFQDN must not be empty")
	}
}

func TestBuildVertexDomain(t *testing.T) {
	tests := []struct {
		name      string
		vertex    string
		pipeline  string
		namespace string
		want      string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "normal case",
			vertex:    "in",
			pipeline:  "e2e-gpu-direct-pipeline",
			namespace: "default",
			want:      "in.e2e-gpu-direct-pipeline.default.vertexdomain.local",
		},
		{
			name:      "longer names within limit",
			vertex:    "filter-resize",
			pipeline:  "my-pipeline",
			namespace: "my-namespace",
			want:      "filter-resize.my-pipeline.my-namespace.vertexdomain.local",
		},
		{
			name:      "empty vertex name",
			vertex:    "",
			pipeline:  "pipeline",
			namespace: "default",
			wantErr:   true,
			errMsg:    "vertex",
		},
		{
			name:      "empty pipeline name",
			vertex:    "in",
			pipeline:  "",
			namespace: "default",
			wantErr:   true,
			errMsg:    "pipeline",
		},
		{
			name:      "empty namespace",
			vertex:    "in",
			pipeline:  "pipeline",
			namespace: "",
			wantErr:   true,
			errMsg:    "namespace",
		},
		{
			name:      "long FQDN succeeds when stored in annotation",
			vertex:    "very-long-vertex-name-that-goes-on",
			pipeline:  "very-long-pipeline-name",
			namespace: "default",
			want:      "very-long-vertex-name-that-goes-on.very-long-pipeline-name.default.vertexdomain.local",
		},
		{
			name:      "non-DNS-compatible character in vertex",
			vertex:    "IN_VALID",
			pipeline:  "pipeline",
			namespace: "default",
			wantErr:   true,
			errMsg:    "DNS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got, err := BuildVertexDomain(tc.vertex, tc.pipeline, tc.namespace)

			// Assert
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errMsg)
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
