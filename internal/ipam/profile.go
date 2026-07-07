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
	"fmt"
	"strings"
)

// ParseProfileName splits a dranet profile name "<namespace>/<name>" into its
// namespace and name components. The convention is established in ADR-0004 and
// implemented by the NumaNetwork controller (numanetwork_controller.go:131).
func ParseProfileName(profile string) (namespace, name string, err error) {
	parts := strings.SplitN(profile, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid profile name %q: must be <namespace>/<name>", profile)
	}
	return parts[0], parts[1], nil
}
