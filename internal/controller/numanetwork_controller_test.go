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

package controller

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// buildScheme builds a *runtime.Scheme containing all types needed by these tests.
func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgoscheme: %v", err)
	}
	if err := numaflowv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("numaflowv1alpha1: %v", err)
	}
	if err := resourcev1.AddToScheme(s); err != nil {
		t.Fatalf("resourcev1: %v", err)
	}
	return s
}

// newNN returns a minimal NumaNetwork for use in tests.
func newNN(ns string) *numaflowv1alpha1.NumaNetwork {
	return &numaflowv1alpha1.NumaNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nn",
			Namespace: ns,
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

// cidrPattern matches IPv4 CIDR values like "192.168.10.0/24" (ADR-0004).
var cidrPattern = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+/\d+$`)

// assertNoIPRangeInParams fails the test if params contains any IP-range entry.
// IP ranges must be resolved by the dranet webhook profile, not embedded in opaque config (ADR-0004).
func assertNoIPRangeInParams(t *testing.T, params map[string]string) {
	t.Helper()
	for k, v := range params {
		if strings.Contains(strings.ToLower(k), "iprange") ||
			strings.Contains(strings.ToLower(k), "cidr") ||
			cidrPattern.MatchString(v) {
			t.Errorf("opaque params must not contain IP-range entry %q=%q (must go through profile/webhook)", k, v)
		}
	}
}

// ─── TestBuildResourceClaimTemplate ───────────────────────────────────────────

func TestBuildResourceClaimTemplate(t *testing.T) {
	nn := newNN("default")

	rct := BuildResourceClaimTemplate(nn)

	// Name convention: <nn.Name>-rct
	if rct.Name != "test-nn-rct" {
		t.Errorf("Name = %q, want %q", rct.Name, "test-nn-rct")
	}
	// Same namespace as NumaNetwork
	if rct.Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", rct.Namespace, "default")
	}
	// DeviceClassName propagated from spec
	if len(rct.Spec.Spec.Devices.Requests) == 0 {
		t.Fatal("Requests is empty")
	}
	req := rct.Spec.Spec.Devices.Requests[0]
	if req.Name != "nic" {
		t.Errorf("request.Name = %q, want %q", req.Name, "nic")
	}
	if req.Exactly == nil || req.Exactly.DeviceClassName != "vf.nvidia.dra.net" {
		var got string
		if req.Exactly != nil {
			got = req.Exactly.DeviceClassName
		}
		t.Errorf("DeviceClassName = %q, want %q", got, "vf.nvidia.dra.net")
	}
	// opaque config must have driver == "dra.net"
	if len(rct.Spec.Spec.Devices.Config) == 0 {
		t.Fatal("Config is empty")
	}
	cfg := rct.Spec.Spec.Devices.Config[0]
	if cfg.Opaque == nil {
		t.Fatal("Opaque is nil")
	}
	if cfg.Opaque.Driver != "dra.net" {
		t.Errorf("Driver = %q, want %q", cfg.Opaque.Driver, "dra.net")
	}
	// parameters.profile must follow <namespace>/<name> convention
	var params map[string]string
	if err := json.Unmarshal(cfg.Opaque.Parameters.Raw, &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	wantProfile := "default/test-nn"
	if params["profile"] != wantProfile {
		t.Errorf("profile = %q, want %q", params["profile"], wantProfile)
	}
	// ipRange must NOT appear directly in opaque parameters (ADR-0004).
	assertNoIPRangeInParams(t, params)
}

// ─── Reconcile integration tests (fake client) ────────────────────────────────

func TestReconcile_RCTCreatedInSameNamespace(t *testing.T) {
	// Arrange
	s := buildScheme(t)
	nn := newNN("test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Assert: RCT created in same namespace
	rct := &resourcev1.ResourceClaimTemplate{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-nn-rct", Namespace: "test-ns"}, rct); err != nil {
		t.Fatalf("RCT not found: %v", err)
	}
	if rct.Namespace != "test-ns" {
		t.Errorf("RCT.Namespace = %q, want %q", rct.Namespace, "test-ns")
	}
}

func TestReconcile_OwnerReference(t *testing.T) {
	// Arrange
	s := buildScheme(t)
	nn := newNN("test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Assert: ownerReference points to NumaNetwork
	rct := &resourcev1.ResourceClaimTemplate{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-nn-rct", Namespace: "test-ns"}, rct); err != nil {
		t.Fatalf("RCT not found: %v", err)
	}
	if len(rct.OwnerReferences) == 0 {
		t.Fatal("OwnerReferences is empty")
	}
	owner := rct.OwnerReferences[0]
	if owner.Kind != "NumaNetwork" {
		t.Errorf("owner.Kind = %q, want %q", owner.Kind, "NumaNetwork")
	}
	if owner.Name != "test-nn" {
		t.Errorf("owner.Name = %q, want %q", owner.Name, "test-nn")
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Error("owner.Controller must be true")
	}
}

func TestReconcile_StatusUpdated(t *testing.T) {
	// Arrange
	s := buildScheme(t)
	nn := newNN("test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Assert: status.resourceClaimTemplateName is set
	updated := &numaflowv1alpha1.NumaNetwork{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}, updated); err != nil {
		t.Fatalf("NumaNetwork not found: %v", err)
	}
	if updated.Status.ResourceClaimTemplateName != "test-nn-rct" {
		t.Errorf("Status.ResourceClaimTemplateName = %q, want %q", updated.Status.ResourceClaimTemplateName, "test-nn-rct")
	}
}

func TestReconcile_Idempotent(t *testing.T) {
	// Arrange
	s := buildScheme(t)
	nn := newNN("test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}}

	// Act: reconcile twice
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first Reconcile error: %v", err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second Reconcile error: %v", err)
	}

	// Assert: only one RCT exists
	rctList := &resourcev1.ResourceClaimTemplateList{}
	if err := fakeClient.List(context.Background(), rctList); err != nil {
		t.Fatalf("list RCTs: %v", err)
	}
	if len(rctList.Items) != 1 {
		t.Errorf("RCT count = %d, want 1 (idempotency check)", len(rctList.Items))
	}
}

func TestReconcile_OpaqueDriverAndProfile(t *testing.T) {
	// Arrange
	s := buildScheme(t)
	nn := newNN("test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Assert: opaque driver == "dra.net" and profile follows convention
	rct := &resourcev1.ResourceClaimTemplate{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-nn-rct", Namespace: "test-ns"}, rct); err != nil {
		t.Fatalf("RCT not found: %v", err)
	}
	if len(rct.Spec.Spec.Devices.Config) == 0 || rct.Spec.Spec.Devices.Config[0].Opaque == nil {
		t.Fatal("Opaque config missing")
	}
	opaque := rct.Spec.Spec.Devices.Config[0].Opaque
	if opaque.Driver != "dra.net" {
		t.Errorf("Driver = %q, want %q", opaque.Driver, "dra.net")
	}
	var params map[string]string
	if err := json.Unmarshal(opaque.Parameters.Raw, &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if params["profile"] != "test-ns/test-nn" {
		t.Errorf("profile = %q, want %q", params["profile"], "test-ns/test-nn")
	}
}

func TestReconcile_IPRangeNotInOpaqueConfig(t *testing.T) {
	// Arrange
	s := buildScheme(t)
	nn := newNN("test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Assert: ipRange / CIDR must not appear directly in opaque config (ADR-0004)
	rct := &resourcev1.ResourceClaimTemplate{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-nn-rct", Namespace: "test-ns"}, rct); err != nil {
		t.Fatalf("RCT not found: %v", err)
	}
	if len(rct.Spec.Spec.Devices.Config) == 0 || rct.Spec.Spec.Devices.Config[0].Opaque == nil {
		t.Fatal("Opaque config missing")
	}
	var params map[string]string
	if err := json.Unmarshal(rct.Spec.Spec.Devices.Config[0].Opaque.Parameters.Raw, &params); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	// IP range must not appear directly in opaque config (ADR-0004).
	assertNoIPRangeInParams(t, params)
}

func TestReconcile_NotFound_NoError(t *testing.T) {
	// Arrange: NumaNetwork does not exist
	s := buildScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act: reconciling a missing object should return no error
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing", Namespace: "default"}})

	// Assert
	if err != nil {
		t.Errorf("expected no error for NotFound, got: %v", err)
	}
}

func TestReconcile_RCTSpecUpdatedOnChange(t *testing.T) {
	// Arrange: pre-create RCT with wrong DeviceClass, reconcile should update it
	s := buildScheme(t)
	nn := newNN("test-ns")
	// Pre-create RCT with a stale spec
	staleRCT := BuildResourceClaimTemplate(nn)
	staleRCT.Spec.Spec.Devices.Requests[0].Exactly.DeviceClassName = "old.class"
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(nn, staleRCT).WithStatusSubresource(nn).Build()
	r := &NumaNetworkReconciler{Client: fakeClient, Scheme: s}

	// Act
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-nn", Namespace: "test-ns"}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Assert: DeviceClass updated to match NumaNetwork spec
	rct := &resourcev1.ResourceClaimTemplate{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-nn-rct", Namespace: "test-ns"}, rct); err != nil {
		t.Fatalf("RCT not found: %v", err)
	}
	got := rct.Spec.Spec.Devices.Requests[0].Exactly.DeviceClassName
	if got != "vf.nvidia.dra.net" {
		t.Errorf("DeviceClassName = %q after update, want %q", got, "vf.nvidia.dra.net")
	}
	// Assert: ownerReference must be restored even on the update path.
	if len(rct.OwnerReferences) == 0 {
		t.Fatal("OwnerReferences is empty after update — ownerRef must be maintained on update path")
	}
	owner := rct.OwnerReferences[0]
	if owner.Kind != "NumaNetwork" || owner.Name != "test-nn" {
		t.Errorf("owner = {Kind:%q Name:%q}, want {Kind:NumaNetwork Name:test-nn}", owner.Kind, owner.Name)
	}
}
