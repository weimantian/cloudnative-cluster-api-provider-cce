/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"slices"

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
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
)

// MachinePoolFinalizer ensures the CCE node pool is deleted before the object.
const MachinePoolFinalizer = "ccemanagedmachinepool.infrastructure.cluster.x-k8s.io"

// CCEManagedMachinePoolReconciler reconciles CCEManagedMachinePool objects
// (InfrastructureMachinePool). It drives the CCE node pool lifecycle.
type CCEManagedMachinePoolReconciler struct {
	client.Client

	// ServiceFactory builds the CCE API service for a region/credential pair.
	// Overridden in tests with a fake; defaults to cceService.NewClient.
	ServiceFactory func(regionID, ak, sk string) (cceService.Service, error)
}

// newCCEService returns a CCE service via the injected factory, or the real
// implementation when no factory is set.
func (r *CCEManagedMachinePoolReconciler) newCCEService(regionID, ak, sk string) (cceService.Service, error) {
	if r.ServiceFactory != nil {
		return r.ServiceFactory(regionID, ak, sk)
	}
	return cceService.NewClient(regionID, ak, sk)
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=get;list;watch

// Reconcile implements the reconcile loop of CCEManagedMachinePool.
func (r *CCEManagedMachinePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	pool := &infrav1beta1.CCEManagedMachinePool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, pool.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get cluster of machine pool %s", req.Name)
	}
	if cluster == nil {
		log.Info("Machine pool has no cluster reference yet")
		return ctrl.Result{}, nil
	}

	if annotations.IsPaused(cluster, pool) {
		log.Info("Machine pool is paused")
		return ctrl.Result{}, nil
	}

	if !pool.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster, pool)
	}

	return r.reconcileNormal(ctx, cluster, pool)
}

func (r *CCEManagedMachinePoolReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, pool *infrav1beta1.CCEManagedMachinePool) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Node pools can only be created once the control plane is Available
	// (official: CreateNodePool requires an Available/Scaling cluster).
	cp := &controlplanev1beta1.CCEManagedControlPlane{}
	if cluster.Spec.ControlPlaneRef.Name != "" {
		if err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to get control plane")
		}
	} else {
		return ctrl.Result{}, errors.New("cluster has no controlPlaneRef")
	}
	if !cp.Status.Ready || cp.Status.ClusterID == "" {
		log.Info("Control plane is not ready yet, waiting")
		conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
			conditions.WaitingForControlPlaneReason, "")
		if err := r.Status().Update(ctx, pool); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	if controllerutil.AddFinalizer(pool, MachinePoolFinalizer) {
		if err := r.Update(ctx, pool); err != nil {
			return ctrl.Result{}, err
		}
	}

	region, err := r.clusterRegion(ctx, cluster, pool)
	if err != nil {
		conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, pool); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}
	creds, err := scope.ResolveCredentials(ctx, r.Client, pool.Namespace, pool.Spec.ClusterName+"-credentials")
	if err != nil {
		conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, pool); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}
	svc, err := r.newCCEService(region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, pool); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}

	// Ensure the node pool exists.
	clusterID := cp.Status.ClusterID
	if pool.Status.NodePoolID == "" {
		id, err := svc.CreateNodePool(ctx, toCreateNodePoolInput(clusterID, pool))
		if err != nil {
			conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			if uerr := r.Status().Update(ctx, pool); uerr != nil {
				return ctrl.Result{}, uerr
			}
			return ctrl.Result{}, err
		}
		pool.Status.NodePoolID = id
		// The create call already bound the security groups (and autoscaling
		// when the gate is on), so record them as applied to avoid a redundant
		// UpdateNodePool on the next reconcile.
		pool.Status.LastAppliedSecurityGroups = append([]string(nil), pool.Spec.SecurityGroups...)
		pool.Status.LastAppliedAutoscaling = pool.Spec.Autoscaling
	}

	// Reconcile scale: align the pool's expected count with the MachinePool
	// replicas. desiredNodeCount is the ABSOLUTE expected total (official docs;
	// final live-test confirmation is questionnaire Q3), so pass replicas
	// directly. status.replicas is refreshed from the cloud below.
	if pool.Status.Replicas != pool.Spec.Replicas {
		conditions.MarkFalse(pool, conditions.NodePoolScalingCondition,
			conditions.ReconciliationInProgressReason, "scaling node pool")
		if err := svc.ScaleNodePool(ctx, clusterID, pool.Status.NodePoolID, pool.Spec.Replicas); err != nil {
			conditions.MarkFalse(pool, conditions.NodePoolScalingCondition,
				conditions.ReconciliationFailedReason, err.Error())
			if uerr := r.Status().Update(ctx, pool); uerr != nil {
				return ctrl.Result{}, uerr
			}
			return ctrl.Result{}, err
		}
		conditions.MarkTrue(pool, conditions.NodePoolScalingCondition, "ScalingCompleted", "node pool scaled")
	}

	// Reconcile mutable attributes (currently: security groups, Q5; and
	// autoscaling when the NodePoolAutoscaling gate is on) without touching
	// the expected node count. UpdateNodePool omitting initialNodeCount
	// defaults it to 0 and SHRINKS the pool (official cce_02_0356,
	// questionnaire Q3), so IgnoreInitialNodeCount must be set.
	attrsChanged := !slices.Equal(pool.Status.LastAppliedSecurityGroups, pool.Spec.SecurityGroups)
	if features.Enabled(features.NodePoolAutoscaling) && pool.Spec.Autoscaling != pool.Status.LastAppliedAutoscaling {
		attrsChanged = true
	}
	if attrsChanged {
		update := cceService.UpdateNodePoolInput{
			ClusterID:              clusterID,
			NodePoolID:             pool.Status.NodePoolID,
			IgnoreInitialNodeCount: true,
			CustomSecurityGroups:   append([]string(nil), pool.Spec.SecurityGroups...),
		}
		if features.Enabled(features.NodePoolAutoscaling) {
			update.Autoscaling = toProviderAutoscaling(pool.Spec.Autoscaling)
		}
		// When the spec declares taints/labels, sync them onto existing nodes
		// too (official taint/labelPolicyOnExistingNodes=refresh) — guards
		// against drift such as a node reset that cleared user taints/labels
		// (questionnaire Q11b, cce_10_0198).
		if len(pool.Spec.Taints) > 0 {
			update.TaintPolicyOnExistingNodes = "refresh"
		}
		if len(pool.Spec.Labels) > 0 {
			update.LabelPolicyOnExistingNodes = "refresh"
		}
		if err := svc.UpdateNodePool(ctx, update); err != nil {
			conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			if uerr := r.Status().Update(ctx, pool); uerr != nil {
				return ctrl.Result{}, uerr
			}
			return ctrl.Result{}, err
		}
		pool.Status.LastAppliedSecurityGroups = append([]string(nil), pool.Spec.SecurityGroups...)
		pool.Status.LastAppliedAutoscaling = pool.Spec.Autoscaling
		log.Info("Node pool attributes updated (security groups / autoscaling / taints / labels)", "nodePoolID", pool.Status.NodePoolID)
	}

	// Refresh observed state from the cloud (Active node count is a
	// verification item — questionnaire Q3).
	pools, err := svc.ListNodePools(ctx, clusterID)
	if err != nil {
		conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, pool); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}
	// Replicas should reflect the ACTUAL node count, not the desired target
	// (spec.initialNodeCount) — status.currentNode is the expected total,
	// status.activeNode is the ready count (official NodePoolStatus). Mark a
	// pool whose node pool disappeared out-of-band as NotReady so it is
	// reconciled (recreated) instead of scaling a deleted pool forever.
	found := false
	for _, p := range pools {
		if p.NodePoolID == pool.Status.NodePoolID {
			found = true
			pool.Status.Replicas = p.NodeCount
			pool.Status.AvailableReplicas = p.ActiveNodeCount
		}
	}
	if !found {
		conditions.MarkFalse(pool, conditions.NodePoolReadyCondition,
			conditions.ReconciliationFailedReason, "node pool not found in cloud, recreating")
		pool.Status.NodePoolID = ""
		if err := r.Status().Update(ctx, pool); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	conditions.MarkTrue(pool, conditions.NodePoolReadyCondition, "NodePoolReady", "node pool is ready")
	pool.Status.Ready = true
	log.Info("CCE node pool reconciled", "nodePoolID", pool.Status.NodePoolID)
	// Persist status explicitly (status subresource ignores r.Update).
	if err := r.Status().Update(ctx, pool); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *CCEManagedMachinePoolReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, pool *infrav1beta1.CCEManagedMachinePool) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	if pool.Status.NodePoolID != "" {
		region, err := r.clusterRegion(ctx, cluster, pool)
		if err != nil {
			return ctrl.Result{}, err
		}
		creds, err := scope.ResolveCredentials(ctx, r.Client, pool.Namespace, pool.Spec.ClusterName+"-credentials")
		if err != nil {
			return ctrl.Result{}, err
		}
		svc, err := r.newCCEService(region, creds.AccessKey, creds.SecretKey)
		if err != nil {
			return ctrl.Result{}, err
		}
		// The cluster ID is read from the control plane status.
		cp := &controlplanev1beta1.CCEManagedControlPlane{}
		if cluster.Spec.ControlPlaneRef.Name != "" {
			if err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err != nil {
				return ctrl.Result{}, err
			}
		}
		if cp.Status.ClusterID != "" {
			// Idempotent delete + wait: keep requesting deletion until the
			// node pool is actually gone from the cloud, then clear the ID so
			// the finalizer can be removed (fix: the ID was never cleared,
			// dead-locking deletion forever).
			pools, err := svc.ListNodePools(ctx, cp.Status.ClusterID)
			if err != nil {
				return ctrl.Result{}, err
			}
			stillExists := false
			for _, p := range pools {
				if p.NodePoolID == pool.Status.NodePoolID {
					stillExists = true
					break
				}
			}
			if stillExists {
				if err := svc.DeleteNodePool(ctx, cp.Status.ClusterID, pool.Status.NodePoolID); err != nil {
					return ctrl.Result{}, err
				}
				log.Info("Node pool deletion requested, waiting", "nodePoolID", pool.Status.NodePoolID)
				return ctrl.Result{RequeueAfter: defaultRequeue}, nil
			}
			pool.Status.NodePoolID = ""
		}
	}

	controllerutil.RemoveFinalizer(pool, MachinePoolFinalizer)
	if err := r.Update(ctx, pool); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *CCEManagedMachinePoolReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1beta1.CCEManagedMachinePool{}).
		Named("ccemanagedmachinepool").
		Complete(r)
}

// ---- helpers ----

func (r *CCEManagedMachinePoolReconciler) clusterRegion(ctx context.Context, cluster *clusterv1.Cluster, pool *infrav1beta1.CCEManagedMachinePool) (string, error) {
	if cluster.Spec.InfrastructureRef.Name == "" {
		return "", errors.New("cluster has no infrastructureRef")
	}
	cceCluster := &infrav1beta1.CCECluster{}
	key := types.NamespacedName{Namespace: pool.Namespace, Name: cluster.Spec.InfrastructureRef.Name}
	if err := r.Get(ctx, key, cceCluster); err != nil {
		return "", errors.Wrapf(err, "failed to get CCECluster %s", key)
	}
	return cceCluster.Spec.Region, nil
}

func toCreateNodePoolInput(clusterID string, pool *infrav1beta1.CCEManagedMachinePool) cceService.CreateNodePoolInput {
	in := cceService.CreateNodePoolInput{
		ClusterID:        clusterID,
		Name:             pool.Spec.NodePoolName,
		Flavor:           pool.Spec.Flavor,
		OS:               pool.Spec.OS,
		SSHKey:           pool.Spec.SSHKey,
		AvailabilityZone: pool.Spec.AvailabilityZone,
		InitialNodeCount: pool.Spec.Replicas,
		BillingMode:      pool.Spec.BillingMode,
		Taints:           pool.Spec.Taints,
		Labels:           pool.Spec.Labels,
		SecurityGroups:   pool.Spec.SecurityGroups,
	}
	if features.Enabled(features.NodePoolAutoscaling) {
		in.Autoscaling = toProviderAutoscaling(pool.Spec.Autoscaling)
	}
	if pool.Spec.RootVolume != nil {
		in.RootVolumeSize = pool.Spec.RootVolume.Size
		in.RootVolumeType = pool.Spec.RootVolume.Type
	}
	if len(pool.Spec.DataVolumes) > 0 {
		in.DataVolumeSize = pool.Spec.DataVolumes[0].Size
		in.DataVolumeType = pool.Spec.DataVolumes[0].Type
	}
	return in
}

// toProviderAutoscaling maps the spec autoscaling to the service input.
// Called only when the NodePoolAutoscaling gate is enabled.
func toProviderAutoscaling(s infrav1beta1.AutoscalingSpec) *cceService.NodePoolAutoscaling {
	return &cceService.NodePoolAutoscaling{
		Enable:       s.Enable,
		MinNodeCount: s.MinNodeCount,
		MaxNodeCount: s.MaxNodeCount,
	}
}
