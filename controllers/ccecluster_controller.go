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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/network"
)

// CCEClusterFinalizer ensures the shell object is released only after the
// dependent cloud resources (control plane / node pools) are gone.
const CCEClusterFinalizer = "ccecluster.infrastructure.cluster.x-k8s.io"

// defaultRequeue is the requeue interval for in-progress operations.
const defaultRequeue = 30 * time.Second

type CCEClusterReconciler struct {
	client.Client
	// Recorder emits Kubernetes events for this reconciler (wired via
	// mgr.GetEventRecorderFor in SetupControllers). Nil in tests.
	Recorder record.EventRecorder

	// NetworkValidatorFactory builds the network validator for a
	// region/credential pair. Overridden in tests with a fake; defaults to
	// network.NewValidator (see SetupControllers).
	NetworkValidatorFactory func(regionID string, creds *credentials.Credentials) (network.ValidatorInterface, error)

	// NetworkServiceFactory builds the managed-network service (VPC/
	// subnets/NAT create+delete) for a region/credential pair. Overridden
	// in tests with a fake; defaults to network.NewManager.
	NetworkServiceFactory func(regionID string, creds *credentials.Credentials) (network.ManagerInterface, error)

	// CredentialProvider resolves temporary security credentials for an
	// agency-based identity. Nil means agency identities cannot be assumed
	// (static AK/SK only). Injected in SetupControllers; nil in tests.
	CredentialProvider credentials.Provider
}

// newNetworkValidator returns a validator via the injected factory, or the
// real implementation when no factory is set.
func (r *CCEClusterReconciler) newNetworkValidator(regionID string, creds *credentials.Credentials) (network.ValidatorInterface, error) {
	if r.NetworkValidatorFactory != nil {
		return r.NetworkValidatorFactory(regionID, creds)
	}
	return network.NewValidator(regionID, creds)
}

// newNetworkService returns a managed-network service via the injected
// factory, or the real implementation when no factory is set.
func (r *CCEClusterReconciler) newNetworkService(regionID string, creds *credentials.Credentials) (network.ManagerInterface, error) {
	if r.NetworkServiceFactory != nil {
		return r.NetworkServiceFactory(regionID, creds)
	}
	return network.NewManager(regionID, creds)
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile implements the reconcile loop of CCECluster using the
// per-reconcile CCEClusterScope (CAPA pkg/cloud/scope pattern).
func (r *CCEClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	cceCluster := &infrav1beta2.CCECluster{}
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

	scope, err := scope.NewCCEClusterScope(scope.CCEClusterScopeParams{
		Client:         r.Client,
		Cluster:        cluster,
		CCECluster:     cceCluster,
		ControllerName: "ccecluster",
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to build CCECluster scope")
	}
	defer func() {
		if err := scope.Close(ctx); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if cluster == nil {
		log.Info("Cluster controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	if annotations.IsPaused(cluster, cceCluster) {
		log.Info("CCECluster is paused")
		return ctrl.Result{}, nil
	}

	if !cceCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster, cceCluster)
	}

	return r.reconcileNormal(ctx, cluster, cceCluster)
}

func (r *CCEClusterReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, cceCluster *infrav1beta2.CCECluster) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	controllerutil.AddFinalizer(cceCluster, CCEClusterFinalizer)

	if cceCluster.Spec.Region == "" {
		conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, "spec.region is required")
		recordEvent(r.Recorder, cceCluster, corev1.EventTypeWarning, "NetworkValidationFailed", "spec.region is required")
		return ctrl.Result{}, errors.New("spec.region is required")
	}

	// Resolve credentials through the control plane's identityRef chain
	// (identityRef -> per-cluster Secret -> env) so identityRef-based clusters
	// are not stuck on network validation/managed reconciliation - mirroring
	// resolveControlPlaneCredentials, which the CP/MP controllers use. A
	// credentials resolution FAILURE must not silently skip validation.
	creds, agency, credErr := r.resolveClusterCredentials(ctx, cluster, cceCluster)
	if credErr != nil {
		conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, credErr.Error())
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}
	if creds != nil {
		resolved, rerr := credentials.Resolve(ctx, r.CredentialProvider, cceCluster.Spec.Region, agency, creds.AccessKey, creds.SecretKey)
		if rerr != nil {
			conditions.MarkFalse(cceCluster,
					conditions.NetworkReadyCondition,
					conditions.NetworkValidationFailedReason, rerr.Error())
			return ctrl.Result{RequeueAfter: requeueAfterForError(rerr)}, nil
		}
		// Managed network mode (vpc.id empty): create the VPC/subnets/(NAT)
		// first, then validate the result. BYO mode validates the referenced
		// network as before.
		if network.IsManaged(&cceCluster.Spec.Network, cluster.Name) {
			svc, serr := r.newNetworkService(cceCluster.Spec.Region, resolved)
			if serr != nil {
				conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, serr.Error())
				return ctrl.Result{RequeueAfter: requeueAfterForError(serr)}, nil
			}
			if rerr := r.reconcileManagedNetwork(ctx, cceCluster, cluster.Name, svc); rerr != nil {
				conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, rerr.Error())
				recordEvent(r.Recorder, cceCluster, corev1.EventTypeWarning, "ManagedNetworkFailed", "%v", rerr)
				return ctrl.Result{RequeueAfter: requeueAfterForError(rerr)}, nil
			}
			recordEvent(r.Recorder, cceCluster, corev1.EventTypeNormal, "ManagedNetworkReconciled",
				"managed network reconciled (VPC %s)", cceCluster.Spec.Network.VPC.ResourceID)
		}
		validator, verr := r.newNetworkValidator(cceCluster.Spec.Region, resolved)
		if verr != nil {
			conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, verr.Error())
			return ctrl.Result{RequeueAfter: requeueAfterForError(verr)}, nil
		}
		// Read the container/service CIDR from the control plane spec.
		containerMode, containerCIDR, serviceCIDR, eniSubnets := "", "", "", []string{}
		if cluster.Spec.ControlPlaneRef.Name != "" {
			cp := &controlplanev1beta2.CCEManagedControlPlane{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: cceCluster.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err == nil {
				containerMode = cp.Spec.ContainerNetwork.Mode
				containerCIDR = cp.Spec.ContainerNetwork.CIDR
				serviceCIDR = cp.Spec.ServiceNetwork.CIDR
				eniSubnets = cp.Spec.ContainerNetwork.ENISubnets
			}
		}
		issues, verr := validator.Validate(ctx, network.ValidateInput{
			VPCID:         effectiveVPCID(cceCluster),
			SubnetIDs:     subnetIDs(cceCluster),
			ContainerMode: containerMode,
			ContainerCIDR: containerCIDR,
			ServiceCIDR:   serviceCIDR,
			ENISubnetIDs:  eniSubnets,
		})
		if verr != nil {
			conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, verr.Error())
			return ctrl.Result{RequeueAfter: requeueAfterForError(verr)}, nil
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
			conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, strings.Join(hardMsgs, "; "))
			recordEvent(r.Recorder, cceCluster, corev1.EventTypeWarning, "NetworkValidationFailed", "%s", strings.Join(hardMsgs, "; "))
			// Persist the failure condition (status subresource ignores
			// r.Update).
			return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
		}
	}
	conditions.MarkTrue(cceCluster, conditions.NetworkReadyCondition, "NetworkValidated", "network references validated")
	recordEvent(r.Recorder, cceCluster, corev1.EventTypeNormal, "NetworkValidated", "network references validated")

	// Ready condition + contract provisioned flag (CAPI v1beta2 contract):
	// the CAPI Cluster controller gates
	// Cluster.Status.Initialization.InfrastructureProvisioned on
	// status.initialization.provisioned of the infrastructure cluster.
	conditions.MarkTrue(cceCluster, clusterv1.ReadyCondition, "InfrastructureReady", "CCE infrastructure is ready")
	cceCluster.Status.Initialization.Provisioned = true

	// Backfill the CCE cluster ID from the control plane when available.
	if cluster.Spec.ControlPlaneRef.Name != "" {
		cp := &controlplanev1beta2.CCEManagedControlPlane{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: cceCluster.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err == nil && cp.Status.ClusterID != "" {
			cceCluster.Status.ClusterID = cp.Status.ClusterID
		}
	}

	cceCluster.Status.Ready = true
	log.Info("CCECluster infrastructure is ready")
	return ctrl.Result{}, nil
}

// effectiveVPCID returns the VPC the cluster consumes: the referenced BYO
// id, or the provider-created resource id in managed mode.
func effectiveVPCID(cceCluster *infrav1beta2.CCECluster) string {
	if cceCluster.Spec.Network.VPC.ID != "" {
		return cceCluster.Spec.Network.VPC.ID
	}
	return cceCluster.Spec.Network.VPC.ResourceID
}

// subnetIDs extracts the node-subnet IDs from the CCECluster spec: the BYO
// id when referencing, the provider-created ResourceID in managed mode.
// ENI subnets (type eni) are excluded - they carry container traffic.
func subnetIDs(cceCluster *infrav1beta2.CCECluster) []string {
	var ids []string
	for _, s := range cceCluster.Spec.Network.Subnets {
		if s.Type == common.SubnetTypeENI {
			continue
		}
		id := s.ID
		if id == "" {
			id = s.ResourceID
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *CCEClusterReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, cceCluster *infrav1beta2.CCECluster) (ctrl.Result, error) {
	// Cloud resources (CCE cluster / node pools) are owned and deleted by the
	// control plane and machine pool controllers; here we only release the
	// shell object once they are gone. The control plane and machine pool
	// delete paths read this object for region/VPC, so we must wait for the
	// control plane to disappear before removing our finalizer - otherwise
	// their deletion would loop on NotFound and orphan cloud resources.
	if cluster.Spec.ControlPlaneRef.Name != "" {
		cp := &controlplanev1beta2.CCEManagedControlPlane{}
		err := r.Get(ctx, types.NamespacedName{Namespace: cceCluster.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp)
		if err == nil {
			// Control plane still exists - wait for it to be deleted first.
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	// Managed network teardown (BYO networks are never touched): runs
	// strictly after the control plane is gone so the CCE cluster no longer
	// consumes the VPC. Deletion is idempotent (per-resource NotFound is
	// tolerated); aggregated errors requeue until the whole network is gone.
	if network.IsManaged(&cceCluster.Spec.Network, cluster.Name) {
		if cceCluster.Spec.Network.VPC.ResourceID != "" {
			creds, agency, cerr := r.resolveClusterCredentials(ctx, cluster, cceCluster)
			if cerr != nil || creds == nil {
				conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, "managed network deletion requires credentials: "+errStr(cerr))
				return ctrl.Result{}, errors.New("managed network deletion requires credentials")
			}
			resolved, rerr := credentials.Resolve(ctx, r.CredentialProvider, cceCluster.Spec.Region, agency, creds.AccessKey, creds.SecretKey)
			if rerr != nil {
				return ctrl.Result{RequeueAfter: defaultRequeue}, nil
			}
			svc, serr := r.newNetworkService(cceCluster.Spec.Region, resolved)
			if serr != nil {
				return ctrl.Result{RequeueAfter: defaultRequeue}, nil
			}
			if derr := svc.DeleteNetwork(ctx, &cceCluster.Spec.Network, cluster.Name); derr != nil {
				conditions.MarkFalse(cceCluster,
				conditions.NetworkReadyCondition,
				conditions.NetworkValidationFailedReason, derr.Error())
				recordEvent(r.Recorder, cceCluster, corev1.EventTypeWarning, "ManagedNetworkDeleteFailed", "%v", derr)
				return ctrl.Result{RequeueAfter: defaultRequeue}, nil
			}
			recordEvent(r.Recorder, cceCluster, corev1.EventTypeNormal, "ManagedNetworkDeleted",
				"managed network deleted (VPC %s)", cceCluster.Spec.Network.VPC.ResourceID)
		}
	}
	controllerutil.RemoveFinalizer(cceCluster, CCEClusterFinalizer)
	return ctrl.Result{}, nil
}

// resolveClusterCredentials resolves credentials for the CCECluster shell
// through the control plane's identityRef chain when a control plane exists,
// else the per-cluster Secret (mirrors resolveControlPlaneCredentials, which
// the CP/MP controllers use, so identityRef-based clusters are not stuck).
func (r *CCEClusterReconciler) resolveClusterCredentials(ctx context.Context, cluster *clusterv1.Cluster, cceCluster *infrav1beta2.CCECluster) (*scope.Credentials, string, error) {
	if cluster.Spec.ControlPlaneRef.Name != "" {
		cp := &controlplanev1beta2.CCEManagedControlPlane{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: cceCluster.Namespace, Name: cluster.Spec.ControlPlaneRef.Name}, cp); err == nil {
			creds, agency, err := resolveControlPlaneCredentials(ctx, r.Client, cp)
			return creds, agency, err
		}
	}
	creds, err := scope.ResolveCredentials(ctx, r.Client, cceCluster.Namespace, cluster.Name+"-credentials")
	return creds, "", err
}

// reconcileManagedNetwork drives the managed-network lifecycle step by step,
// marking a dedicated condition per step (mirrors CAPA VpcReady/SubnetsReady/
// NatGatewaysReady) so operators see intermediate progress.
func (r *CCEClusterReconciler) reconcileManagedNetwork(ctx context.Context, cceCluster *infrav1beta2.CCECluster, clusterName string, svc network.ManagerInterface) error {
	spec := &cceCluster.Spec.Network
	if err := svc.ReconcileVpc(ctx, spec, clusterName); err != nil {
		conditions.MarkFalse(cceCluster,
				conditions.VpcReadyCondition,
				conditions.NetworkReconciliationFailedReason, err.Error())
		return err
	}
	conditions.MarkTrue(cceCluster, conditions.VpcReadyCondition, "VpcReconciled", "managed VPC reconciled")
	if err := svc.ReconcileSubnets(ctx, spec, clusterName); err != nil {
		conditions.MarkFalse(cceCluster,
				conditions.SubnetsReadyCondition,
				conditions.NetworkReconciliationFailedReason, err.Error())
		return err
	}
	conditions.MarkTrue(cceCluster, conditions.SubnetsReadyCondition, "SubnetsReconciled", "managed subnets reconciled")
	if spec.NatGateway != nil {
		if err := svc.ReconcileNatGateway(ctx, spec, clusterName); err != nil {
			conditions.MarkFalse(cceCluster,
				conditions.NatGatewaysReadyCondition,
				conditions.NetworkReconciliationFailedReason, err.Error())
			return err
		}
		conditions.MarkTrue(cceCluster, conditions.NatGatewaysReadyCondition, "NatGatewayReconciled", "managed NAT gateway reconciled")
	}
	if spec.SecurityGroup != nil {
		if err := svc.ReconcileSecurityGroup(ctx, spec, clusterName); err != nil {
			conditions.MarkFalse(cceCluster,
				conditions.SecurityGroupsReadyCondition,
				conditions.NetworkReconciliationFailedReason, err.Error())
			return err
		}
		conditions.MarkTrue(cceCluster, conditions.SecurityGroupsReadyCondition, "SecurityGroupsReconciled", "managed security group reconciled")
	}
	return nil
}

// errStr renders an error for messages; empty when nil (used where a
// nil error must not print as "<nil>").
func errStr(err error) string {
	if err == nil {
		return "none"
	}
	return err.Error()
}

// SetupWithManager registers the controller with the manager.
func (r *CCEClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1beta2.CCECluster{}).
		WithOptions(opts).
		Named("ccecluster").
		Complete(r)
}
