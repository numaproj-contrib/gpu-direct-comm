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

package ipam

import (
	"encoding/json"
	"testing"
)

func TestBuildConfFromNuma(t *testing.T) {
	tests := []struct {
		name        string
		networkName string
		ipRange     string
		wantError   bool
	}{
		{
			name:        "valid CIDR",
			networkName: "test-net",
			ipRange:     "192.168.10.0/24",
		},
		{
			name:        "valid /32 single host",
			networkName: "single",
			ipRange:     "10.0.0.1/32",
		},
		{
			name:        "empty ipRange",
			networkName: "test-net",
			ipRange:     "",
			wantError:   true,
		},
		{
			name:        "empty networkName",
			networkName: "",
			ipRange:     "192.168.10.0/24",
			wantError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildConfFromNuma(tt.networkName, tt.ipRange)
			if tt.wantError {
				if err == nil {
					t.Errorf("BuildConfFromNuma(%q, %q) = nil error, want error", tt.networkName, tt.ipRange)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildConfFromNuma(%q, %q) error = %v", tt.networkName, tt.ipRange, err)
			}

			// Verify JSON structure
			var conf map[string]any
			if err := json.Unmarshal(got, &conf); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			// cniVersion must be present
			if v, ok := conf["cniVersion"]; !ok || v == "" {
				t.Error("cniVersion missing or empty")
			}

			// name must match networkName
			if v := conf["name"]; v != tt.networkName {
				t.Errorf("name = %v, want %q", v, tt.networkName)
			}

			// ipam.type must be "whereabouts"
			ipam, ok := conf["ipam"].(map[string]any)
			if !ok {
				t.Fatal("ipam section missing or not an object")
			}
			if ipam["type"] != "whereabouts" {
				t.Errorf("ipam.type = %v, want %q", ipam["type"], "whereabouts")
			}

			// ipam.range must match ipRange
			if ipam["range"] != tt.ipRange {
				t.Errorf("ipam.range = %v, want %q", ipam["range"], tt.ipRange)
			}
		})
	}
}
