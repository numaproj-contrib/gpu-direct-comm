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
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/numaproj-contrib/gpu-direct-comm/internal/dns"
	webhookv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/internal/webhook/v1alpha1"
)

const (
	vertexDomainFinalizer = "gpu-direct-comm.numaproj.io/vertex-domain-dns"
	requeueDelay          = 5 * time.Second
)

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=update
// +kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaims,verbs=get;list;watch

// VertexDomainReconciler watches Pods with the vertexDomain label and
// registers/removes DNS A records in the CoreDNS etcd backend.
type VertexDomainReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Store  dns.Store
}

func (r *VertexDomainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Pod: %w", err)
	}

	if _, hasLabel := pod.Labels[webhookv1alpha1.LabelVertexDomain]; !hasLabel {
		return ctrl.Result{}, nil
	}

	fqdn := pod.Annotations[webhookv1alpha1.AnnotationVertexDomainFQDN]
	if fqdn == "" {
		log.V(1).Info("vertex-domain label present but FQDN annotation missing, skipping")
		return ctrl.Result{}, nil
	}

	// Handle deletion: remove DNS record, then remove finalizer.
	if !pod.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(pod, vertexDomainFinalizer) {
			if err := r.Store.Delete(ctx, fqdn); err != nil {
				return ctrl.Result{}, fmt.Errorf("delete DNS record for %s: %w", fqdn, err)
			}
			log.Info("deleted DNS record", "fqdn", fqdn)

			controllerutil.RemoveFinalizer(pod, vertexDomainFinalizer)
			if err := r.Update(ctx, pod); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(pod, vertexDomainFinalizer) {
		controllerutil.AddFinalizer(pod, vertexDomainFinalizer)
		if err := r.Update(ctx, pod); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	// Extract Secondary NIC IP from ResourceClaim status.
	ip, err := r.extractSecondaryNICIP(ctx, pod)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ip == "" {
		log.V(1).Info("IP not yet available, requeuing", "fqdn", fqdn)
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	if err := r.Store.Put(ctx, fqdn, ip); err != nil {
		return ctrl.Result{}, fmt.Errorf("put DNS record for %s: %w", fqdn, err)
	}
	log.Info("registered DNS record", "fqdn", fqdn, "ip", ip)

	return ctrl.Result{}, nil
}

// extractSecondaryNICIP finds the first IP from ResourceClaim NetworkData
// associated with this Pod. Returns empty string if not yet available.
func (r *VertexDomainReconciler) extractSecondaryNICIP(ctx context.Context, pod *corev1.Pod) (string, error) {
	for _, claimStatus := range pod.Status.ResourceClaimStatuses {
		if claimStatus.ResourceClaimName == nil {
			continue
		}

		claim := &resourcev1.ResourceClaim{}
		key := types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      *claimStatus.ResourceClaimName,
		}
		if err := r.Get(ctx, key, claim); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("get ResourceClaim %s: %w", key.Name, err)
		}

		for _, device := range claim.Status.Devices {
			if device.NetworkData == nil {
				continue
			}
			for _, cidr := range device.NetworkData.IPs {
				ip, _, err := net.ParseCIDR(cidr)
				if err != nil {
					ip = net.ParseIP(cidr)
				}
				if ip != nil {
					return ip.String(), nil
				}
			}
		}
	}
	return "", nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VertexDomainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	labelExists := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[webhookv1alpha1.LabelVertexDomain]
		return ok
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(labelExists).
		Named("vertexdomain").
		Complete(r)
}
