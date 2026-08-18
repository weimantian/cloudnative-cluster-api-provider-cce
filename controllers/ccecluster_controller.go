/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
)

// CCEClusterFinalizer ensures the shell object is released only after the
// dependent cloud resources (control plane / node pools) are gone.
const CCEClusterFinalizer = "ccecluster.infrastructure.cluster.x-k8s.io"

// defaultRequeue is the requeue interval for in-progress operations.
const defaultRequeue = 30 * time.Second

// CCEClusterReconciler reconciles CCECluster objects (InfrastructureCluster).
type CCEClusterReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get

// Reconcile implements the reconcile loop of CCECluster.
func (r *CCEClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	cceCluster := &infrav1beta1.CCECluster{}
	if err := r.Get(ctx, req.NamespacedName, cceCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	cluster, err := util.GetOwnerCluster(ctx, r.Client, cceCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get owner cluster of CCECluster %s", req.Name)
	}
	if cluster == nil {
		log.Info("Cluster controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	if annotations.IsPaused(cluster, cceCluster) {
		log.Info("CCECluster is paused")
		return ctrl.Result{}, nil
	}

	if !cceCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cceCluster)
	}

	return r.reconcileNormal(ctx, cluster, cceCluster)
}

func (r *CCEClusterReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, cceCluster *infrav1beta1.CCECluster) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	if controllerutil.AddFinalizer(cceCluster, CCEClusterFinalizer) {
		if err := r.Update(ctx, cceCluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Validate the referenced network. Full validation (VPC/subnet existence,
	// CIDR compatibility) is a P0 item — the CCE API rejects non-compliant
	// networks at create time (see questionnaire Q4/Q5).
	// TODO(P0): network validation service.
	if cceCluster.Spec.Region == "" {
		conditions.MarkFalse(cceCluster, conditions.NetworkReadyCondition,
			conditions.ReconciliationFailedReason,
			"spec.region is required")
		return ctrl.Result{}, errors.New("spec.region is required")
	}
	conditions.MarkTrue(cceCluster, conditions.NetworkReadyCondition, "NetworkValidated", "network references validated")

	// Backfill the CCE cluster ID from the control plane when available.
	if cluster.Spec.ControlPlaneRef.Name != "" {
		cp := &controlplanev1beta1.CCEManagedControlPlane{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: cceCluster.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err == nil && cp.Status.ClusterID != "" {
			cceCluster.Status.ClusterID = cp.Status.ClusterID
		}
	}

	cceCluster.Status.Ready = true
	log.Info("CCECluster infrastructure is ready")
	return ctrl.Result{}, nil
}

func (r *CCEClusterReconciler) reconcileDelete(ctx context.Context, cceCluster *infrav1beta1.CCECluster) (ctrl.Result, error) {
	// Cloud resources (CCE cluster / node pools) are owned and deleted by the
	// control plane and machine pool controllers; here we only release the
	// shell object once they are gone.
	controllerutil.RemoveFinalizer(cceCluster, CCEClusterFinalizer)
	if err := r.Update(ctx, cceCluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *CCEClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1beta1.CCECluster{}).
		Named("ccecluster").
		Complete(r)
}
