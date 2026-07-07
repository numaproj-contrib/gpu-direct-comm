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
	"testing"
)

func TestParseProfileName(t *testing.T) {
	tests := []struct {
		name      string
		profile   string
		wantNS    string
		wantName  string
		wantError bool
	}{
		{
			name:     "valid simple",
			profile:  "default/my-network",
			wantNS:   "default",
			wantName: "my-network",
		},
		{
			name:     "valid with hyphens and numbers",
			profile:  "kube-system/net-01",
			wantNS:   "kube-system",
			wantName: "net-01",
		},
		{
			name:      "empty string",
			profile:   "",
			wantError: true,
		},
		{
			name:      "no slash",
			profile:   "just-a-name",
			wantError: true,
		},
		{
			name:      "empty namespace",
			profile:   "/my-network",
			wantError: true,
		},
		{
			name:      "empty name",
			profile:   "default/",
			wantError: true,
		},
		{
			name:      "too many slashes",
			profile:   "a/b/c",
			wantError: true,
		},
		{
			name:      "only slash",
			profile:   "/",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, name, err := ParseProfileName(tt.profile)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseProfileName(%q) = (%q, %q, nil), want error", tt.profile, ns, name)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseProfileName(%q) error = %v, want nil", tt.profile, err)
				return
			}
			if ns != tt.wantNS {
				t.Errorf("namespace = %q, want %q", ns, tt.wantNS)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}
