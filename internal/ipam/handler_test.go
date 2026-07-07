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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// fakeExecutor implements CNIExecutor for testing.
type fakeExecutor struct {
	addIP  net.IP
	addErr error
	delErr error
	// captured state
	lastConf     []byte
	lastClaimUID string
	addCalled    bool
	delCalled    bool
}

func (f *fakeExecutor) Add(_ context.Context, conf []byte, claimUID string) (net.IP, error) {
	f.addCalled = true
	f.lastConf = conf
	f.lastClaimUID = claimUID
	return f.addIP, f.addErr
}

func (f *fakeExecutor) Del(_ context.Context, conf []byte, claimUID string) error {
	f.delCalled = true
	f.lastConf = conf
	f.lastClaimUID = claimUID
	return f.delErr
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := numaflowv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("numaflowv1alpha1: %v", err)
	}
	return s
}

func testNumaNetwork() *numaflowv1alpha1.NumaNetwork {
	return &numaflowv1alpha1.NumaNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nn",
			Namespace: "test-ns",
		},
		Spec: numaflowv1alpha1.NumaNetworkSpec{
			RefDeviceClass: numaflowv1alpha1.RefDeviceClass{
				Name: "vf.nvidia.dra.net",
			},
			RefResourceClaimDranet: numaflowv1alpha1.RefResourceClaimDranet{
				IPRange: "192.168.10.0/24",
			},
		},
	}
}

func makeProfileRequest(profile, claimUID string) []byte {
	req := ProfileRequest{
		ClaimUID: claimUID,
		Config:   &NetworkConfig{Profile: profile},
	}
	b, _ := json.Marshal(req)
	return b
}

func TestGetProfileConfig(t *testing.T) {
	tests := []struct {
		name       string
		body       []byte
		executor   *fakeExecutor
		seedNN     bool
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid profile with existing numaNetwork",
			body:       makeProfileRequest("test-ns/test-nn", "claim-123"),
			executor:   &fakeExecutor{addIP: net.ParseIP("192.168.10.5")},
			seedNN:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid profile - no slash",
			body:       makeProfileRequest("just-a-name", "claim-123"),
			executor:   &fakeExecutor{},
			seedNN:     true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "numaNetwork not found",
			body:       makeProfileRequest("test-ns/missing-nn", "claim-123"),
			executor:   &fakeExecutor{},
			seedNN:     true,
			wantStatus: http.StatusNotFound,
			wantBody:   "test-ns/missing-nn",
		},
		{
			name:       "executor Add error",
			body:       makeProfileRequest("test-ns/test-nn", "claim-123"),
			executor:   &fakeExecutor{addErr: fmt.Errorf("pool exhausted")},
			seedNN:     true,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testScheme(t)
			builder := fake.NewClientBuilder().WithScheme(s)
			if tt.seedNN {
				builder = builder.WithObjects(testNumaNetwork())
			}
			c := builder.Build()

			h := &Handler{Client: c, Executor: tt.executor}
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)

			req := httptest.NewRequest(http.MethodPost, "/GetProfileConfig", bytes.NewReader(tt.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantBody != "" && !strings.Contains(w.Body.String(), tt.wantBody) {
				t.Errorf("body %q does not contain %q", w.Body.String(), tt.wantBody)
			}

			if tt.wantStatus == http.StatusOK {
				if !tt.executor.addCalled {
					t.Error("executor.Add was not called")
				}
				if tt.executor.lastClaimUID != "claim-123" {
					t.Errorf("claimUID = %q, want %q", tt.executor.lastClaimUID, "claim-123")
				}

				// Verify conf passed to executor contains the ipRange
				var conf map[string]any
				if err := json.Unmarshal(tt.executor.lastConf, &conf); err != nil {
					t.Fatalf("unmarshal conf: %v", err)
				}
				ipamSection, ok := conf["ipam"].(map[string]any)
				if !ok {
					t.Fatal("ipam section missing in conf")
				}
				if ipamSection["range"] != "192.168.10.0/24" {
					t.Errorf("ipam.range = %v, want %q", ipamSection["range"], "192.168.10.0/24")
				}

				// Verify response contains allocated IP
				var resp NetworkConfig
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal response: %v", err)
				}
				if len(resp.Interface.Addresses) == 0 || !strings.HasPrefix(resp.Interface.Addresses[0], "192.168.10.5") {
					t.Errorf("response addresses = %v, want prefix %q", resp.Interface.Addresses, "192.168.10.5")
				}
			}
		})
	}
}

func TestReleaseProfileConfig(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(testNumaNetwork()).Build()
	exec := &fakeExecutor{}

	h := &Handler{Client: c, Executor: exec}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := makeProfileRequest("test-ns/test-nn", "claim-456")
	req := httptest.NewRequest(http.MethodPost, "/ReleaseProfileConfig", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
	}
	if !exec.delCalled {
		t.Error("executor.Del was not called")
	}
	if exec.lastClaimUID != "claim-456" {
		t.Errorf("claimUID = %q, want %q", exec.lastClaimUID, "claim-456")
	}
}

func TestHealth(t *testing.T) {
	h := &Handler{}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var caps Capabilities
	if err := json.Unmarshal(w.Body.Bytes(), &caps); err != nil {
		t.Fatalf("unmarshal capabilities: %v", err)
	}
	if !caps.ProfileProvider {
		t.Error("profileProvider should be true")
	}
	if caps.CloudProvider {
		t.Error("cloudProvider should be false")
	}
}
