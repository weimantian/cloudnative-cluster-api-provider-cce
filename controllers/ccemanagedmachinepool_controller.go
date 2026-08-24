/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"slices"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
)

// MachinePoolFinalizer ensures the CCE node pool is deleted before the object.
const MachinePoolFinalizer = "ccemanagedmachinepool.infrastructure.cluster.x-k8s.io"

// CCEManagedMachinePoolReconciler reconciles CCEManagedMachinePool objects
// (InfrastructureMachinePool). It drives the CCE node pool lifecycle.
type CCEManagedMachinePoolReconciler struct {
	client.Client
	// Recorder emits Kubernetes events for this reconciler (wired via
	// mgr.GetEventRecorderFor in SetupControllers). Nil in tests.
	Recorder record.EventRecorder

	// ServiceFactory builds the CCE API service for a region/credential pair.
	// Overridden in tests with a fake; defaults to cceService.NewClient.
	ServiceFactory func(regionID string, creds *credentials.Credentials) (cceService.Service, error)

	// CredentialProvider resolves temporary security credentials for an
	// agency-based identity. Nil means agency identities cannot be assumed
	// (static AK/SK only). Injected in SetupControllers; nil in tests.
	CredentialProvider credentials.Provider
}

// newCCEService returns a CCE service via the injected factory, or the real
// implementation when no factory is set.
func (r *CCEManagedMachinePoolReconciler) newCCEService(regionID string, creds *credentials.Credentials) (cceService.Service, error) {
	if r.ServiceFactory != nil {
		return r.ServiceFactory(regionID, creds)
	}
	return cceService.NewClient(regionID, creds)
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ccemanagedmachinepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile implements the reconcile loop of CCEManagedMachinePool using
// the per-reconcile CCEManagedMachinePoolScope (CAPA pkg/cloud/scope pattern).
// The scope's PatchObject() (called via defer) atomically updates
// status.observedGeneration via patch.WithStatusObservedGeneration (CAPA
// commit 9e9bb6b31).
func (r *CCEManagedMachinePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	pool := &infrav1beta2.CCEManagedMachinePool{}
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
		res, err := r.reconcileDelete(ctx, cluster, pool)
		return res, err
	}

	// Build the per-reconcile scope (constructor builds the patchHelper and
	// snapshots Status.ObservedGeneration for coalesced-event detection).
	scope, err := scope.NewCCEManagedMachinePoolScope(scope.CCEManagedMachinePoolScopeParams{
		Client:                 r.Client,
		Cluster:                cluster,
		CCEManagedMachinePool: pool,
		ControllerName:         "ccemanagedmachinepool",
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to build CMP scope")
	}
	defer func() {
		if err := scope.Close(ctx); err != nil && reterr == nil {
			reterr = err
		}
	}()

	res, err = r.reconcileNormal(ctx, cluster, pool)
	if err != nil {
		return res, err
	}

	// CAPA b5d6d3081: requeue when observed generation is behind current.
	if scope.ObservedGenerationAtStart() < scope.GenerationAtStart() {
		log.Info("Observed generation behind current generation, requeueing",
			"observedGeneration", scope.ObservedGenerationAtStart(),
			"generation", scope.GenerationAtStart())
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}
	return res, nil
}

func (r *CCEManagedMachinePoolReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, pool *infrav1beta2.CCEManagedMachinePool) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Node pools can only be created once the control plane is Available
	// (official: CreateNodePool requires an Available/Scaling cluster).
	cp := &controlplanev1beta2.CCEManagedControlPlane{}
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
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	controllerutil.AddFinalizer(pool, MachinePoolFinalizer)

	region, err := r.clusterRegion(ctx, cluster, pool)
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	// Credentials come from the control plane's identity chain (identityRef
	// -> per-cluster Secret -> env), shared with the control plane
	// controller: machine pools must honor identityRef too, otherwise pools
	// of identity-based clusters never reconcile.
	creds, identityAgency, err := resolveControlPlaneCredentials(ctx, r.Client, cp)
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	resolved, err := credentials.Resolve(ctx, r.CredentialProvider, region, identityAgency, creds.AccessKey, creds.SecretKey)
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	svc, err := r.newCCEService(region, resolved)
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	// Ensure the node pool exists.
	clusterID := cp.Status.ClusterID
	if pool.Status.NodePoolID == "" {
		id, err := svc.CreateNodePool(ctx, toCreateNodePoolInput(clusterID, pool))
		if err != nil {
			conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		pool.Status.NodePoolID = id
		recordEvent(r.Recorder, pool, corev1.EventTypeNormal, "NodePoolCreated", "created CCE node pool %s", id)
		// The pool was just created with initialNodeCount == spec.replicas, so
		// record the desired count as the observed count too. Otherwise the
		// scale check below sees 0 != replicas and issues a redundant
		// ScaleNodePool that the platform rejects with "No scale task needed
		// with desired node count N" (verified live).
		pool.Status.Replicas = pool.Spec.Replicas
		// The create call already bound the security groups (and autoscaling
		// when the gate is on), so record them as applied to avoid a redundant
		// UpdateNodePool on the next reconcile.
		pool.Status.LastAppliedSecurityGroups = append([]string(nil), pool.Spec.SecurityGroups...)
		pool.Status.LastAppliedAutoscaling = pool.Spec.Autoscaling
	}

	// Replicas are driven by the owning CAPI MachinePool (kubectl scale
	// machinepool). When the owner carries the external-autoscaler annotation
	// (cluster.x-k8s.io/replicas-managed-by), the provider does NOT drive the
	// count - it reverse-syncs the CAPI MachinePool.spec.replicas from the
	// cloud-side desired count instead (mirrors CAPA eks/nodegroup.go).
	mp, err := r.findOwnerMachinePool(ctx, pool)
	if err != nil {
		return ctrl.Result{}, err
	}
	externallyManaged := mp != nil && annotations.ReplicasManagedByExternalAutoscaler(mp)
	if externallyManaged {
		recordEvent(r.Recorder, pool, corev1.EventTypeNormal, "ReplicasManagedExternally", "replicas are managed by an external autoscaler; reverse-syncing from the cloud")
	} else {
		if err := r.syncReplicasFromOwner(ctx, pool); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile scale: align the pool's expected count with the MachinePool
	// replicas. desiredNodeCount is the ABSOLUTE expected total (official docs;
	// final live-test confirmation is questionnaire Q3), so pass replicas
	// directly. status.replicas is refreshed from the cloud below. Externally
	// managed pools never receive a ScaleNodePool - their spec.replicas is
	// reverse-synced from the cloud after the status refresh.
	if !externallyManaged && pool.Status.Replicas != pool.Spec.Replicas {
		conditions.MarkFalse(pool, conditions.NodePoolScalingCondition,
			conditions.ReconciliationInProgressReason, "scaling node pool")
		if err := svc.ScaleNodePool(ctx, clusterID, pool.Status.NodePoolID, pool.Spec.Replicas); err != nil {
			conditions.MarkFalse(pool,
				conditions.NodePoolScalingCondition,
				conditions.NodePoolScaleFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		conditions.MarkTrue(pool, conditions.NodePoolScalingCondition, "ScalingCompleted", "node pool scaled")
		recordEvent(r.Recorder, pool, corev1.EventTypeNormal, "NodePoolScaled", "scaled node pool to %d nodes", pool.Spec.Replicas)
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
			conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		// Roll the updated attributes onto existing nodes (CCE 同步节点池 /
		// UpgradeNodePool — the analogue of CAPA's UpdateConfig rolling update).
		// UpdateNodePool alone only affects newly created nodes (Q5/Q11b).
		// Default the batch size defensively (do not rely on webhook defaulting,
		// which may be disabled); official range [1,20].
		maxUnavailable := pool.Spec.UpdateConfig.MaxUnavailable
		if maxUnavailable == 0 {
			maxUnavailable = 1
		}
		if err := svc.UpgradeNodePool(ctx, clusterID, pool.Status.NodePoolID, maxUnavailable); err != nil {
			conditions.MarkFalse(pool,
				conditions.NodePoolScalingCondition,
				conditions.NodePoolScaleFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		pool.Status.LastAppliedSecurityGroups = append([]string(nil), pool.Spec.SecurityGroups...)
		pool.Status.LastAppliedAutoscaling = pool.Spec.Autoscaling
		log.Info("Node pool attributes updated and rolled onto existing nodes", "nodePoolID", pool.Status.NodePoolID)
	}

	// Refresh observed state from the cloud (Active node count is a
	// verification item — questionnaire Q3).
	pools, err := svc.ListNodePools(ctx, clusterID)
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
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
			// Externally managed replicas: reverse-sync the CAPI MachinePool.
			// spec.replicas from the cloud-side node count (mirrors CAPA: the
			// external autoscaler is the source of truth, and MachinePool.spec
			// must reflect the cloud's desired count so it does not drift).
			if externallyManaged && mp != nil {
				desired := p.NodeCount
				if mp.Spec.Replicas == nil || *mp.Spec.Replicas != desired {
					original := mp.DeepCopy()
					mp.Spec.Replicas = &desired
					if err := r.Patch(ctx, mp, client.MergeFrom(original)); err != nil {
						return ctrl.Result{}, errors.Wrap(err, "failed to patch MachinePool replicas")
					}
				}
			}
		}
	}
	if !found {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, "node pool not found in cloud, recreating")
		pool.Status.NodePoolID = ""
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	// Populate spec.providerIDList so Cluster API can fill
	// MachinePool.status.nodeRefs (and the deprecated readyReplicas). The
	// controller owns this field, mirroring the huaweicloud:///<serverId>
	// provider IDs. Sorting keeps the slice stable across reconciles to avoid
	// spurious spec churn.
	providerIDs, err := svc.ListNodes(ctx, clusterID, pool.Status.NodePoolID)
	if err != nil {
		conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	slices.Sort(providerIDs)
	pool.Spec.ProviderIDList = providerIDs

	// Node auto-repair (mirrors CAPA NodeRepairConfig.Enabled). CCE has no
	// EKS-style auto-repair switch, so the provider drives repair directly:
	// detect Abnormal/Error nodes and reset them via CCE ResetNode.
	if pool.Spec.NodeRepair != nil && pool.Spec.NodeRepair.Enabled {
		if err := r.reconcileNodeRepair(ctx, svc, clusterID, pool); err != nil {
			conditions.MarkFalse(pool,
				conditions.NodePoolReadyCondition,
				conditions.NodePoolCreationFailedReason, err.Error())
			return ctrl.Result{}, err
		}
	}

	conditions.MarkTrue(pool, conditions.NodePoolReadyCondition, "NodePoolReady", "node pool is ready")
	recordEvent(r.Recorder, pool, corev1.EventTypeNormal, "NodePoolReady", "node pool is ready")
	pool.Status.Ready = true
	log.Info("CCE node pool reconciled", "nodePoolID", pool.Status.NodePoolID)
	// Nodes may still be provisioning after the pool is marked ready: the
	// cloud status (currentNode/activeNode) lags the create call, so a
	// one-shot reconcile can observe 0 and then never refresh. Requeue until
	// the observed count converges to the desired count so status.replicas
	// and status.availableReplicas reflect reality.
	if pool.Status.Replicas != pool.Spec.Replicas || pool.Status.AvailableReplicas != pool.Spec.Replicas {
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *CCEManagedMachinePoolReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, pool *infrav1beta2.CCEManagedMachinePool) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	if pool.Status.NodePoolID != "" {
		// The node pool lives in the CCE cluster created by the control
		// plane, so read the control plane for the cluster ID AND the
		// credential chain. The delete path must honor identityRef exactly
		// like reconcileNormal - resolving only the per-cluster Secret left
		// identity-based clusters stuck in a delete-error loop forever.
		cp := &controlplanev1beta2.CCEManagedControlPlane{}
		cpFound := false
		if cluster.Spec.ControlPlaneRef.Name != "" {
			err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp)
			switch {
			case apierrors.IsNotFound(err):
				// Control plane gone => the cloud cluster was deleted before
				// it (the control plane finalizer guarantees that), so the
				// node pool is gone too. Release without cloud calls instead
				// of erroring forever on NotFound.
			case err != nil:
				return ctrl.Result{}, errors.Wrap(err, "failed to get control plane")
			default:
				cpFound = true
			}
		}
		if cpFound && cp.Status.ClusterID != "" {
			region, err := r.clusterRegion(ctx, cluster, pool)
			if err != nil {
				return ctrl.Result{}, err
			}
			creds, identityAgency, err := resolveControlPlaneCredentials(ctx, r.Client, cp)
			if err != nil {
				return ctrl.Result{}, err
			}
			resolved, err := credentials.Resolve(ctx, r.CredentialProvider, region, identityAgency, creds.AccessKey, creds.SecretKey)
			if err != nil {
				return ctrl.Result{}, err
			}
			svc, err := r.newCCEService(region, resolved)
			if err != nil {
				return ctrl.Result{}, err
			}
			if err != nil {
				return ctrl.Result{}, err
			}
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
				recordEvent(r.Recorder, pool, corev1.EventTypeNormal, "NodePoolDeletionRequested", "deletion requested for CCE node pool %s", pool.Status.NodePoolID)
				return ctrl.Result{RequeueAfter: defaultRequeue}, nil
			}
		}
		// Cloud node pool gone (or the cluster was never created): clear the
		// ID so the finalizer can release, and persist it (status subresource)
		// so a surviving object does not retry deleting a pool that no
		// longer exists.
		pool.Status.NodePoolID = ""
	}

	controllerutil.RemoveFinalizer(pool, MachinePoolFinalizer)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *CCEManagedMachinePoolReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1beta2.CCEManagedMachinePool{}).
		// Watch the owning CAPI MachinePool so replica changes (kubectl scale
		// machinepool) trigger reconciliation of the infrastructure pool:
		// CAPI core does NOT sync spec.replicas onto infrastructure machine
		// pools, so without this watch a scale change would sit unprocessed
		// until an unrelated event reconciles the pool. GenerationChanged-
		// Predicate skips status-only MachinePool updates.
		Watches(
			&clusterv1.MachinePool{},
			handler.EnqueueRequestsFromMapFunc(r.machinePoolToInfraPool),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		WithOptions(opts).
		Named("ccemanagedmachinepool").
		Complete(r)
}

// machinePoolToInfraPool maps a CAPI MachinePool event to the
// CCEManagedMachinePool it references via spec.template.spec.
// infrastructureRef (same namespace). Non-CCE infra refs are ignored.
func (r *CCEManagedMachinePoolReconciler) machinePoolToInfraPool(_ context.Context, obj client.Object) []reconcile.Request {
	mp, ok := obj.(*clusterv1.MachinePool)
	if !ok {
		return nil
	}
	ref := mp.Spec.Template.Spec.InfrastructureRef
	if ref.Kind != "CCEManagedMachinePool" || ref.Name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Namespace: mp.Namespace, Name: ref.Name},
	}}
}

// ---- helpers ----

func (r *CCEManagedMachinePoolReconciler) clusterRegion(ctx context.Context, cluster *clusterv1.Cluster, pool *infrav1beta2.CCEManagedMachinePool) (string, error) {
	if cluster.Spec.InfrastructureRef.Name == "" {
		return "", errors.New("cluster has no infrastructureRef")
	}
	cceCluster := &infrav1beta2.CCECluster{}
	key := types.NamespacedName{Namespace: pool.Namespace, Name: cluster.Spec.InfrastructureRef.Name}
	if err := r.Get(ctx, key, cceCluster); err != nil {
		return "", errors.Wrapf(err, "failed to get CCECluster %s", key)
	}
	return cceCluster.Spec.Region, nil
}

func toCreateNodePoolInput(clusterID string, pool *infrav1beta2.CCEManagedMachinePool) cceService.CreateNodePoolInput {
	in := cceService.CreateNodePoolInput{
		ClusterID:             clusterID,
		ClusterName:           pool.Spec.ClusterName,
		Name:                  pool.Spec.NodePoolName,
		Flavor:                pool.Spec.Flavor,
		OS:                    pool.Spec.OS,
		SSHKey:                pool.Spec.SSHKey,
		AvailabilityZone:      pool.Spec.AvailabilityZone,
		InitialNodeCount:      pool.Spec.Replicas,
		BillingMode:           pool.Spec.BillingMode,
		Spot:                  pool.Spec.Spot,
		SpotPrice:             pool.Spec.SpotPrice,
		Taints:                pool.Spec.Taints,
		Labels:                pool.Spec.Labels,
		SecurityGroups:        pool.Spec.SecurityGroups,
		EcsGroupId:            pool.Spec.EcsGroupId,
		FaultDomain:           pool.Spec.FaultDomain,
		DedicatedHostId:       pool.Spec.DedicatedHostId,
		PreInstall:            pool.Spec.PreInstall,
		PostInstall:           pool.Spec.PostInstall,
		WaitPostInstallFinish: pool.Spec.WaitPostInstallFinish,
	}
	if features.Enabled(features.NodePoolAutoscaling) {
		in.Autoscaling = toProviderAutoscaling(pool.Spec.Autoscaling)
	}
	if pool.Spec.RootVolume != nil {
		in.RootVolumeSize = pool.Spec.RootVolume.Size
		in.RootVolumeType = pool.Spec.RootVolume.Type
	}
	if len(pool.Spec.DataVolumes) > 0 {
		in.DataVolumes = make([]cceService.NodeVolumeInput, 0, len(pool.Spec.DataVolumes))
		for _, v := range pool.Spec.DataVolumes {
			in.DataVolumes = append(in.DataVolumes, cceService.NodeVolumeInput{Size: v.Size, Type: v.Type})
		}
	}
	if len(pool.Spec.ExtensionScaleGroups) > 0 {
		in.ExtensionScaleGroups = make([]cceService.ExtensionScaleGroupInput, 0, len(pool.Spec.ExtensionScaleGroups))
		for _, g := range pool.Spec.ExtensionScaleGroups {
			in.ExtensionScaleGroups = append(in.ExtensionScaleGroups, cceService.ExtensionScaleGroupInput{
				Name:             g.Name,
				Flavor:           g.Flavor,
				AvailabilityZone: g.AvailabilityZone,
			})
		}
	}
	return in
}

// toProviderAutoscaling maps the spec autoscaling to the service input.
// Called only when the NodePoolAutoscaling gate is enabled.
func toProviderAutoscaling(s infrav1beta2.AutoscalingSpec) *cceService.NodePoolAutoscaling {
	return &cceService.NodePoolAutoscaling{
		Enable:       s.Enable,
		MinNodeCount: s.MinNodeCount,
		MaxNodeCount: s.MaxNodeCount,
	}
}

// reconcileNodeRepair detects Abnormal/Error nodes in THIS pool and resets
// them via CCE ResetNode (node auto-repair; the CCE substitute for EKS
// NodeRepairConfig). Nodes are scoped to the pool via metadata.
// ownerReferences.nodepoolID (ListNodes is cluster-wide, so filter by pool).
func (r *CCEManagedMachinePoolReconciler) reconcileNodeRepair(ctx context.Context, svc cceService.Service, clusterID string, pool *infrav1beta2.CCEManagedMachinePool) error {
	nodes, err := svc.ListNodesWithStatus(ctx, clusterID)
	if err != nil {
		return err
	}
	var abnormal []string
	for _, n := range nodes {
		if n.NodePoolID != pool.Status.NodePoolID {
			continue // not ours: another pool's node.
		}
		if n.Phase == "Abnormal" || n.Phase == "Error" {
			abnormal = append(abnormal, n.UID)
		}
	}
	if len(abnormal) == 0 {
		return nil
	}
	recordEvent(r.Recorder, pool, corev1.EventTypeNormal, "NodeRepairTriggered",
		"resetting %d abnormal node(s)", len(abnormal))
	return svc.ResetNode(ctx, clusterID, abnormal)
}

// findOwnerMachinePool returns the owning CAPI MachinePool (found via its
// template.spec.infrastructureRef.name), or nil when none matches yet.
func (r *CCEManagedMachinePoolReconciler) findOwnerMachinePool(ctx context.Context, pool *infrav1beta2.CCEManagedMachinePool) (*clusterv1.MachinePool, error) {
	mps := &clusterv1.MachinePoolList{}
	if err := r.List(ctx, mps, client.InNamespace(pool.Namespace),
		client.MatchingLabels{clusterv1.ClusterNameLabel: pool.Spec.ClusterName}); err != nil {
		return nil, errors.Wrap(err, "failed to list MachinePools")
	}
	for i := range mps.Items {
		mp := &mps.Items[i]
		if mp.Spec.Template.Spec.InfrastructureRef.Name == pool.Name {
			return mp, nil
		}
	}
	return nil, nil
}

// syncReplicasFromOwner copies spec.replicas from the owning CAPI MachinePool
// onto this infra pool, so `kubectl scale machinepool` drives the CCE node
// pool size.
func (r *CCEManagedMachinePoolReconciler) syncReplicasFromOwner(ctx context.Context, pool *infrav1beta2.CCEManagedMachinePool) error {
	mp, err := r.findOwnerMachinePool(ctx, pool)
	if err != nil {
		return err
	}
	if mp != nil && mp.Spec.Replicas != nil && *mp.Spec.Replicas != pool.Spec.Replicas {
		pool.Spec.Replicas = *mp.Spec.Replicas
	}
	return nil
}
