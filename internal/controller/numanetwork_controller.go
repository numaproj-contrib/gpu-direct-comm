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
	"fmt"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
)

// NumaNetworkReconciler reconciles a NumaNetwork object.
type NumaNetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=numaflow.numaproj.io,resources=numanetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=numaflow.numaproj.io,resources=numanetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=numaflow.numaproj.io,resources=numanetworks/finalizers,verbs=update
// +kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaimtemplates,verbs=get;list;watch;create;update;patch;delete

func (r *NumaNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	nn := &numaflowv1alpha1.NumaNetwork{}
	if err := r.Get(ctx, req.NamespacedName, nn); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get NumaNetwork: %w", err)
	}

	if err := r.reconcileRCT(ctx, nn); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciled", "name", nn.Name)
	return ctrl.Result{}, nil
}

// reconcileRCT creates or updates the ResourceClaimTemplate owned by nn,
// then writes the RCT name back to nn.Status.
func (r *NumaNetworkReconciler) reconcileRCT(ctx context.Context, nn *numaflowv1alpha1.NumaNetwork) error {
	desired := BuildResourceClaimTemplate(nn)
	if err := controllerutil.SetControllerReference(nn, desired, r.Scheme); err != nil {
		return fmt.Errorf("set controller reference: %w", err)
	}

	existing := &resourcev1.ResourceClaimTemplate{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)

	// needsUpdate is true when we have an existing object to reconcile against
	// desired — either because Get found it, or because Create raced with
	// another writer (AlreadyExists) and we re-read the winner.
	needsUpdate := false
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil {
			if !apierrors.IsAlreadyExists(createErr) {
				return fmt.Errorf("create RCT: %w", createErr)
			}
			// Race: another writer created the RCT between our Get and Create.
			// Re-read it so the update path below can reconcile spec and ownerRef.
			if err = r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
				return fmt.Errorf("get RCT after AlreadyExists: %w", err)
			}
			needsUpdate = true
		}
	} else if err != nil {
		return fmt.Errorf("get RCT: %w", err)
	} else {
		needsUpdate = true
	}

	if needsUpdate {
		updated := existing.DeepCopy()
		// Restore ownerReference in case it was removed externally.
		if err := controllerutil.SetControllerReference(nn, updated, r.Scheme); err != nil {
			return fmt.Errorf("set controller reference: %w", err)
		}
		updated.Spec = desired.Spec
		if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) ||
			!equality.Semantic.DeepEqual(existing.OwnerReferences, updated.OwnerReferences) {
			if err := r.Update(ctx, updated); err != nil {
				return fmt.Errorf("update RCT: %w", err)
			}
		}
	}

	// Write status only when it changes. Use Patch to avoid resourceVersion
	// conflicts when the spec is updated concurrently (fix 3).
	if nn.Status.ResourceClaimTemplateName != desired.Name {
		patch := nn.DeepCopy()
		patch.Status.ResourceClaimTemplateName = desired.Name
		if err := r.Status().Patch(ctx, patch, client.MergeFrom(nn)); err != nil {
			return fmt.Errorf("update status: %w", err)
		}
	}
	return nil
}

// BuildResourceClaimTemplate builds the ResourceClaimTemplate that corresponds to nn.
// Profile name convention: <namespace>/<name> — embeds namespace so the M4 webhook
// can reverse-lookup the NumaNetwork without a namespace parameter (ADR-0004).
func BuildResourceClaimTemplate(nn *numaflowv1alpha1.NumaNetwork) *resourcev1.ResourceClaimTemplate {
	profileName := nn.Namespace + "/" + nn.Name
	paramsRaw, _ := json.Marshal(map[string]string{"profile": profileName})

	return &resourcev1.ResourceClaimTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name + "-rct",
			Namespace: nn.Namespace,
		},
		Spec: resourcev1.ResourceClaimTemplateSpec{
			Spec: resourcev1.ResourceClaimSpec{
				Devices: resourcev1.DeviceClaim{
					Requests: []resourcev1.DeviceRequest{
						{
							Name: "nic",
							Exactly: &resourcev1.ExactDeviceRequest{
								DeviceClassName: nn.Spec.RefDeviceClass.Name,
							},
						},
					},
					Config: []resourcev1.DeviceClaimConfiguration{
						{
							DeviceConfiguration: resourcev1.DeviceConfiguration{
								Opaque: &resourcev1.OpaqueDeviceConfiguration{
									Driver:     "dra.net",
									Parameters: runtime.RawExtension{Raw: paramsRaw},
								},
							},
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *NumaNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&numaflowv1alpha1.NumaNetwork{}).
		Owns(&resourcev1.ResourceClaimTemplate{}).
		Named("numanetwork").
		Complete(r)
}
