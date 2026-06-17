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

// validEdges is a reusable set of Pipeline spec.edges for test cases.
var validEdges = []PipelineEdge{
	{From: "filter-resize", To: "inference"},
}

// TestParseBindings verifies (a): broken JSON returns an error.
func TestParseBindings(t *testing.T) {
	t.Run("broken JSON returns error", func(t *testing.T) {
		_, err := ParseBindings("not-valid-json")
		if err == nil {
			t.Error("expected error for broken JSON, got nil")
		}
	})

	t.Run("valid JSON parses correctly", func(t *testing.T) {
		raw := `[{"from":"filter-resize","to":"inference","numaNetwork":"nn1","connectionType":"direct"}]`
		bindings, err := ParseBindings(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(bindings) != 1 {
			t.Fatalf("got %d bindings, want 1", len(bindings))
		}
		b := bindings[0]
		if b.From != "filter-resize" || b.To != "inference" || b.NumaNetwork != "nn1" || b.ConnectionType != "direct" {
			t.Errorf("unexpected binding: %+v", b)
		}
	})
}

// TestValidateBindings covers rules (b)(c)(d).
func TestValidateBindings(t *testing.T) {
	tests := []struct {
		name        string
		bindings    []EdgeNumaNetworkBinding
		edges       []PipelineEdge
		wantErr     bool
		errContains string
	}{
		{
			name: "(b) empty from returns error",
			bindings: []EdgeNumaNetworkBinding{
				{From: "", To: "inference", NumaNetwork: "nn1", ConnectionType: "direct"},
			},
			edges:       []PipelineEdge{{From: "", To: "inference"}},
			wantErr:     true,
			errContains: "from and to must not be empty",
		},
		{
			name: "(b) empty to returns error",
			bindings: []EdgeNumaNetworkBinding{
				{From: "filter-resize", To: "", NumaNetwork: "nn1", ConnectionType: "direct"},
			},
			edges:       []PipelineEdge{{From: "filter-resize", To: ""}},
			wantErr:     true,
			errContains: "from and to must not be empty",
		},
		{
			name: "(c) connectionType multi-isbsvc returns error with reason",
			bindings: []EdgeNumaNetworkBinding{
				{From: "filter-resize", To: "inference", NumaNetwork: "nn1", ConnectionType: "multi-isbsvc"},
			},
			edges:       validEdges,
			wantErr:     true,
			errContains: "multi-isbsvc",
		},
		{
			name: "(c) unknown connectionType returns error",
			bindings: []EdgeNumaNetworkBinding{
				{From: "filter-resize", To: "inference", NumaNetwork: "nn1", ConnectionType: "unknown"},
			},
			edges:       validEdges,
			wantErr:     true,
			errContains: "not supported",
		},
		{
			name: "(d) edge not in spec.edges returns error",
			bindings: []EdgeNumaNetworkBinding{
				{From: "a", To: "b", NumaNetwork: "nn1", ConnectionType: "direct"},
			},
			edges:       validEdges,
			wantErr:     true,
			errContains: "not found in Pipeline spec.edges",
		},
		{
			name: "valid binding passes all checks",
			bindings: []EdgeNumaNetworkBinding{
				{From: "filter-resize", To: "inference", NumaNetwork: "nn1", ConnectionType: "direct"},
			},
			edges:   validEdges,
			wantErr: false,
		},
		{
			name:     "empty bindings slice returns nil",
			bindings: []EdgeNumaNetworkBinding{},
			edges:    validEdges,
			wantErr:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange + Act
			err := validateBindings(tc.bindings, tc.edges)

			// Assert
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tc.errContains)
					return
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
