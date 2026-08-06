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
	"fmt"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	webhookv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/internal/webhook/v1alpha1"
)

// mockStore records calls to Put/Get/Delete for test assertions.
type mockStore struct {
	mu      sync.Mutex
	records map[string]string
	putErr  error
	delErr  error
}

func newMockStore() *mockStore {
	return &mockStore{records: make(map[string]string)}
}

func (m *mockStore) Put(_ context.Context, fqdn, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	m.records[fqdn] = ip
	return nil
}

func (m *mockStore) Delete(_ context.Context, fqdn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.delErr != nil {
		return m.delErr
	}
	delete(m.records, fqdn)
	return nil
}

func (m *mockStore) Get(_ context.Context, fqdn string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records[fqdn], nil
}

func (m *mockStore) getRecord(fqdn string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records[fqdn]
}

func (m *mockStore) hasRecord(fqdn string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.records[fqdn]
	return ok
}

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = resourcev1.AddToScheme(s)
	return s
}

func TestVertexDomainReconciler(t *testing.T) {
	const (
		podName   = "e2e-gpu-direct-pipeline-vertex-in-0"
		namespace = "default"
		fqdn      = "in.e2e-gpu-direct-pipeline.default.vertexdomain.local"
		claimName = "test-claim"
		ip        = "192.168.140.10"
		ipCIDR    = "192.168.140.10/24"
	)

	basePod := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels: map[string]string{
					webhookv1alpha1.LabelVertexDomain: fqdn,
				},
			},
			Status: corev1.PodStatus{
				ResourceClaimStatuses: []corev1.PodResourceClaimStatus{
					{Name: "nic", ResourceClaimName: ptr.To(claimName)},
				},
			},
		}
	}

	baseClaim := func() *resourcev1.ResourceClaim {
		return &resourcev1.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: namespace,
			},
			Status: resourcev1.ResourceClaimStatus{
				Devices: []resourcev1.AllocatedDeviceStatus{
					{
						Driver: "dra.net",
						Pool:   "pool",
						Device: "dev",
						NetworkData: &resourcev1.NetworkDeviceData{
							InterfaceName: "dummy0",
							IPs:           []string{ipCIDR},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name        string
		pod         *corev1.Pod
		claim       *resourcev1.ResourceClaim
		storePutErr error
		wantResult  ctrl.Result
		wantErr     bool
		wantPut     bool
		wantIP      string
	}{
		{
			name:    "Pod with IP in ResourceClaim registers DNS record",
			pod:     basePod(),
			claim:   baseClaim(),
			wantPut: true,
			wantIP:  ip,
		},
		{
			name: "Pod without vertexDomain label is skipped",
			pod: func() *corev1.Pod {
				p := basePod()
				delete(p.Labels, webhookv1alpha1.LabelVertexDomain)
				return p
			}(),
			claim:   baseClaim(),
			wantPut: false,
		},
		{
			name: "Pod with no ResourceClaimStatuses requeues",
			pod: func() *corev1.Pod {
				p := basePod()
				p.Status.ResourceClaimStatuses = nil
				return p
			}(),
			claim:      baseClaim(),
			wantPut:    false,
			wantResult: ctrl.Result{RequeueAfter: requeueDelay},
		},
		{
			name: "ResourceClaim with no NetworkData requeues",
			pod:  basePod(),
			claim: func() *resourcev1.ResourceClaim {
				c := baseClaim()
				c.Status.Devices[0].NetworkData = nil
				return c
			}(),
			wantPut:    false,
			wantResult: ctrl.Result{RequeueAfter: requeueDelay},
		},
		{
			name: "ResourceClaim with empty IPs requeues",
			pod:  basePod(),
			claim: func() *resourcev1.ResourceClaim {
				c := baseClaim()
				c.Status.Devices[0].NetworkData.IPs = nil
				return c
			}(),
			wantPut:    false,
			wantResult: ctrl.Result{RequeueAfter: requeueDelay},
		},
		{
			name:        "Store Put error is propagated",
			pod:         basePod(),
			claim:       baseClaim(),
			storePutErr: fmt.Errorf("etcd unavailable"),
			wantErr:     true,
			wantPut:     false,
		},
		{
			name: "Pod not found returns no error",
			pod:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := testScheme()
			store := newMockStore()
			store.putErr = tc.storePutErr

			var objects []runtime.Object
			if tc.pod != nil {
				objects = append(objects, tc.pod)
			}
			if tc.claim != nil {
				objects = append(objects, tc.claim)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			r := &VertexDomainReconciler{
				Client: fakeClient,
				Scheme: scheme,
				Store:  store,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      podName,
				Namespace: namespace,
			}}

			result, err := r.Reconcile(context.Background(), req)

			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tc.wantResult {
				t.Errorf("result = %+v, want %+v", result, tc.wantResult)
			}
			if tc.wantPut {
				if got := store.getRecord(fqdn); got != tc.wantIP {
					t.Errorf("store[%s] = %q, want %q", fqdn, got, tc.wantIP)
				}
			} else if !tc.wantErr && store.hasRecord(fqdn) {
				t.Error("store should not have a record, but it does")
			}
		})
	}
}

func TestVertexDomainReconciler_Deletion(t *testing.T) {
	const (
		podName   = "test-pod"
		namespace = "default"
		fqdn      = "in.pipeline1.default.vertexdomain.local"
	)

	scheme := testScheme()
	store := newMockStore()
	store.records[fqdn] = "192.168.140.10"

	now := metav1.NewTime(time.Now())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              podName,
			Namespace:         namespace,
			DeletionTimestamp: &now,
			Finalizers:        []string{vertexDomainFinalizer},
			Labels: map[string]string{
				webhookv1alpha1.LabelVertexDomain: fqdn,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(pod).
		Build()

	r := &VertexDomainReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Store:  store,
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Name:      podName,
		Namespace: namespace,
	}}

	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("result = %+v, want empty", result)
	}

	// DNS record should be deleted.
	if store.hasRecord(fqdn) {
		t.Error("DNS record should have been deleted")
	}

	// When the last finalizer is removed from a Pod with DeletionTimestamp,
	// the fake client garbage-collects it. Pod not found confirms the
	// finalizer was successfully removed and the Pod was fully deleted.
	updated := &corev1.Pod{}
	err = fakeClient.Get(context.Background(), req.NamespacedName, updated)
	if err == nil {
		for _, f := range updated.Finalizers {
			if f == vertexDomainFinalizer {
				t.Error("finalizer should have been removed")
			}
		}
	}
}

func TestVertexDomainReconciler_StoreDeleteError(t *testing.T) {
	const (
		podName   = "test-pod"
		namespace = "default"
		fqdn      = "in.pipeline1.default.vertexdomain.local"
	)

	scheme := testScheme()
	store := newMockStore()
	store.records[fqdn] = "192.168.140.10"
	store.delErr = fmt.Errorf("etcd unavailable")

	now := metav1.NewTime(time.Now())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              podName,
			Namespace:         namespace,
			DeletionTimestamp: &now,
			Finalizers:        []string{vertexDomainFinalizer},
			Labels: map[string]string{
				webhookv1alpha1.LabelVertexDomain: fqdn,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(pod).
		Build()

	r := &VertexDomainReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Store:  store,
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{
		Name:      podName,
		Namespace: namespace,
	}}

	_, err := r.Reconcile(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from Store.Delete, got nil")
	}

	// Finalizer should NOT be removed when delete fails.
	updated := &corev1.Pod{}
	if err := fakeClient.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("get updated Pod: %v", err)
	}
	found := false
	for _, f := range updated.Finalizers {
		if f == vertexDomainFinalizer {
			found = true
		}
	}
	if !found {
		t.Error("finalizer should remain when Store.Delete fails")
	}
}
