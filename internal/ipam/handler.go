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
	"fmt"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// Handler implements the dranet BYODP webhook endpoints for numaNetwork IP allocation.
type Handler struct {
	Client   client.Client
	Executor CNIExecutor
}

// RegisterRoutes registers the webhook HTTP endpoints on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/GetProfileConfig", h.getProfileConfig)
	mux.HandleFunc("/ReleaseProfileConfig", h.releaseProfileConfig)
	mux.HandleFunc("/health", h.health)
}

func (h *Handler) getProfileConfig(w http.ResponseWriter, r *http.Request) {
	log := logf.FromContext(r.Context())

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}

	profile := ""
	if req.Config != nil {
		profile = req.Config.Profile
	}

	ns, name, err := ParseProfileName(profile)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid profile: %v", err), http.StatusBadRequest)
		return
	}

	nn := &numaflowv1alpha1.NumaNetwork{}
	if err := h.Client.Get(r.Context(), types.NamespacedName{Namespace: ns, Name: name}, nn); err != nil {
		if apierrors.IsNotFound(err) {
			http.Error(w, fmt.Sprintf("numaNetwork %s/%s not found", ns, name), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("get numaNetwork %s/%s: %v", ns, name, err), http.StatusInternalServerError)
		return
	}

	conf, err := BuildConfFromNuma(name, nn.Spec.RefResourceClaimDranet.IPRange)
	if err != nil {
		http.Error(w, fmt.Sprintf("build conf: %v", err), http.StatusInternalServerError)
		return
	}

	ip, err := h.Executor.Add(r.Context(), conf, req.ClaimUID)
	if err != nil {
		http.Error(w, fmt.Sprintf("whereabouts ADD: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("allocated IP", "numaNetwork", ns+"/"+name, "ip", ip.String(), "claimUID", req.ClaimUID)

	resp := &NetworkConfig{
		Interface: InterfaceConfig{
			Addresses: []string{ip.String() + "/" + extractPrefix(nn.Spec.RefResourceClaimDranet.IPRange)},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "encode response")
	}
}

func (h *Handler) releaseProfileConfig(w http.ResponseWriter, r *http.Request) {
	log := logf.FromContext(r.Context())

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}

	profile := ""
	if req.Config != nil {
		profile = req.Config.Profile
	}

	ns, name, err := ParseProfileName(profile)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid profile: %v", err), http.StatusBadRequest)
		return
	}

	nn := &numaflowv1alpha1.NumaNetwork{}
	if err := h.Client.Get(r.Context(), types.NamespacedName{Namespace: ns, Name: name}, nn); err != nil {
		if apierrors.IsNotFound(err) {
			http.Error(w, fmt.Sprintf("numaNetwork %s/%s not found", ns, name), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("get numaNetwork %s/%s: %v", ns, name, err), http.StatusInternalServerError)
		return
	}

	conf, err := BuildConfFromNuma(name, nn.Spec.RefResourceClaimDranet.IPRange)
	if err != nil {
		http.Error(w, fmt.Sprintf("build conf: %v", err), http.StatusInternalServerError)
		return
	}

	if err := h.Executor.Del(r.Context(), conf, req.ClaimUID); err != nil {
		http.Error(w, fmt.Sprintf("whereabouts DEL: %v", err), http.StatusInternalServerError)
		return
	}

	log.Info("released IP", "numaNetwork", ns+"/"+name, "claimUID", req.ClaimUID)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Capabilities{
		CloudProvider:   false,
		ProfileProvider: true,
	})
}

// extractPrefix extracts the prefix length from a CIDR string (e.g. "192.168.10.0/24" → "24").
func extractPrefix(cidr string) string {
	for i := len(cidr) - 1; i >= 0; i-- {
		if cidr[i] == '/' {
			return cidr[i+1:]
		}
	}
	return "32"
}
