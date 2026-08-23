/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// CCEClusterControllerIdentityReconciler ensures the "default"
// CCEClusterControllerIdentity singleton exists, mirroring CAPA's
// AutoControllerIdentityCreator (registered only when the
// AutoControllerIdentityCreator feature gate is on).
type CCEClusterControllerIdentityReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclustercontrolleridentities,verbs=get;list;watch;create

// Reconcile is triggered by CCEManagedControlPlane events. The singleton is
// cluster-scoped, so the namespaced request only tells us a control plane
// exists; we ensure the global "default" identity is present.
func (r *CCEClusterControllerIdentityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cp := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Clusters using static/role identities carry their own credentials; only
	// clusters using (or defaulting to) the controller identity need the
	// singleton.
	if cp.Spec.IdentityRef != nil {
		switch cp.Spec.IdentityRef.Kind {
		case "CCEClusterStaticIdentity", "CCEClusterRoleIdentity":
			return ctrl.Result{}, nil
		}
	}

	existing := &infrav1beta2.CCEClusterControllerIdentity{}
	err := r.Get(ctx, types.NamespacedName{Name: infrav1beta2.CCEClusterControllerIdentityName}, existing)
	if err == nil {
		return ctrl.Result{}, nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// AllowedNamespaces left nil (= any namespace): the default identity is
	// the global fallback, usable from any namespace.
	identity := &infrav1beta2.CCEClusterControllerIdentity{
		TypeMeta: metav1.TypeMeta{
			APIVersion: infrav1beta2.GroupVersion.String(),
			Kind:       "CCEClusterControllerIdentity",
		},
		ObjectMeta: metav1.ObjectMeta{Name: infrav1beta2.CCEClusterControllerIdentityName},
	}
	if err := r.Create(ctx, identity); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler with the manager.
func (r *CCEClusterControllerIdentityReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1beta2.CCEManagedControlPlane{}).
		Named("cceclustercontrolleridentity").
		Complete(r)
}
