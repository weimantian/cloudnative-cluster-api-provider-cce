/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"strings"
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
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/network"
)

// CCEClusterFinalizer ensures the shell object is released only after the
// dependent cloud resources (control plane / node pools) are gone.
const CCEClusterFinalizer = "ccecluster.infrastructure.cluster.x-k8s.io"

// defaultRequeue is the requeue interval for in-progress operations.
const defaultRequeue = 30 * time.Second

// CCEClusterReconciler reconciles CCECluster objects (InfrastructureCluster).
type CCEClusterReconciler struct {
	client.Client

	// NetworkValidatorFactory builds the network validator for a
	// region/credential pair. Overridden in tests with a fake; defaults to
	// network.NewValidator (see SetupControllers).
	NetworkValidatorFactory func(regionID, ak, sk string) (network.ValidatorInterface, error)
}

// newNetworkValidator returns a validator via the injected factory, or the
// real implementation when no factory is set.
func (r *CCEClusterReconciler) newNetworkValidator(regionID, ak, sk string) (network.ValidatorInterface, error) {
	if r.NetworkValidatorFactory != nil {
		return r.NetworkValidatorFactory(regionID, ak, sk)
	}
	return network.NewValidator(regionID, ak, sk)
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

	if cceCluster.Spec.Region == "" {
		conditions.MarkFalse(cceCluster, conditions.NetworkReadyCondition,
			conditions.ReconciliationFailedReason,
			"spec.region is required")
		return ctrl.Result{}, errors.New("spec.region is required")
	}

	// Validate the referenced network (VPC/subnet existence and CIDR
	// compatibility) per the official rules (questionnaire Q4/Q7). When no
	// credentials are configured yet (env fallback missing) the validation is
	// skipped with a warning — the CCE API will reject bad networks at create
	// time (CCE.01400002/01400005).
	if creds, err := scope.ResolveCredentials(ctx, r.Client, cceCluster.Namespace, cluster.Name+"-credentials"); err == nil {
		validator, verr := r.newNetworkValidator(cceCluster.Spec.Region, creds.AccessKey, creds.SecretKey)
		if verr != nil {
			conditions.MarkFalse(cceCluster, conditions.NetworkReadyCondition,
				conditions.ReconciliationFailedReason, verr.Error())
			return ctrl.Result{RequeueAfter: requeueAfterForError(verr)}, verr
		}
		// Read the container/service CIDR from the control plane spec.
		containerMode, containerCIDR, serviceCIDR, eniSubnets := "", "", "", []string{}
		if cluster.Spec.ControlPlaneRef.Name != "" {
			cp := &controlplanev1beta1.CCEManagedControlPlane{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: cceCluster.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err == nil {
				containerMode = cp.Spec.ContainerNetwork.Mode
				containerCIDR = cp.Spec.ContainerNetwork.CIDR
				serviceCIDR = cp.Spec.ServiceNetwork.CIDR
				eniSubnets = cp.Spec.ContainerNetwork.ENISubnets
			}
		}
		issues, verr := validator.Validate(ctx, network.ValidateInput{
			VPCID:         cceCluster.Spec.Network.VPC.ID,
			SubnetIDs:     subnetIDs(cceCluster),
			ContainerMode: containerMode,
			ContainerCIDR: containerCIDR,
			ServiceCIDR:   serviceCIDR,
			ENISubnetIDs:  eniSubnets,
		})
		if verr != nil {
			conditions.MarkFalse(cceCluster, conditions.NetworkReadyCondition,
				conditions.ReconciliationFailedReason, verr.Error())
			return ctrl.Result{RequeueAfter: requeueAfterForError(verr)}, verr
		}
		var hardMsgs []string
		for _, i := range issues {
			if i.Warning {
				log.Info("Network validation warning", "field", i.Field, "message", i.Message)
				continue
			}
			hardMsgs = append(hardMsgs, i.Field+": "+i.Message)
		}
		if len(hardMsgs) > 0 {
			conditions.MarkFalse(cceCluster, conditions.NetworkReadyCondition,
				conditions.ReconciliationFailedReason, strings.Join(hardMsgs, "; "))
			// Persist the failure condition (status subresource ignores
			// r.Update).
			if err := r.Status().Update(ctx, cceCluster); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 2 * time.Minute}, errors.New("network validation failed: " + strings.Join(hardMsgs, "; "))
		}
	} else {
		log.Info("Skipping network validation: no credentials configured (env fallback missing)")
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
	// Persist status explicitly (status subresource ignores r.Update).
	if err := r.Status().Update(ctx, cceCluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// subnetIDs extracts the referenced subnet IDs from the CCECluster spec.
func subnetIDs(cceCluster *infrav1beta1.CCECluster) []string {
	var ids []string
	for _, s := range cceCluster.Spec.Network.Subnets {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids
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
