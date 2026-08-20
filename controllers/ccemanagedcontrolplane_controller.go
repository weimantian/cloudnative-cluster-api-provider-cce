/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// ControlPlaneFinalizer ensures the CCE cluster is deleted before the object.
const ControlPlaneFinalizer = "ccemanagedcontrolplane.controlplane.cluster.x-k8s.io"

// kubeconfigValidityDays is the requested certificate validity (questionnaire
// Q2: -1 or [1,1827]; 365 = one year).
const kubeconfigValidityDays = 365

// CCEManagedControlPlaneReconciler reconciles CCEManagedControlPlane objects
// (ControlPlane). It drives the CCE cluster lifecycle and kubeconfig Secret.
type CCEManagedControlPlaneReconciler struct {
	client.Client
	// Recorder emits Kubernetes events for this reconciler (wired via
	// mgr.GetEventRecorderFor in SetupControllers). Nil in tests.
	Recorder record.EventRecorder

	// ServiceFactory builds the CCE API service for a region/credential pair.
	// Overridden in tests with a fake; defaults to cceService.NewClient
	// (see SetupControllers).
	ServiceFactory func(regionID, ak, sk string) (cceService.Service, error)
}

// newCCEService returns a CCE service via the injected factory, or the real
// implementation when no factory is set.
func (r *CCEManagedControlPlaneReconciler) newCCEService(regionID, ak, sk string) (cceService.Service, error) {
	if r.ServiceFactory != nil {
		return r.ServiceFactory(regionID, ak, sk)
	}
	return cceService.NewClient(regionID, ak, sk)
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the reconcile loop of CCEManagedControlPlane.
func (r *CCEManagedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	cp := &controlplanev1beta1.CCEManagedControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patchHelper, err := patch.NewHelper(cp, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		if err := patchHelper.Patch(ctx, cp); err != nil && reterr == nil {
			reterr = err
		}
	}()

	cluster, err := util.GetOwnerCluster(ctx, r.Client, cp.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get owner cluster of control plane %s", req.Name)
	}
	if cluster == nil {
		log.Info("Cluster controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	if annotations.IsPaused(cluster, cp) {
		log.Info("Control plane is paused")
		return ctrl.Result{}, nil
	}

	if !cp.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster, cp)
	}

	return r.reconcileNormal(ctx, cluster, cp)
}

func (r *CCEManagedControlPlaneReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Wait for the CCECluster shell to report ready (CAPI v1beta2 contract:
	// Cluster.Status.Initialization.InfrastructureProvisioned).
	infraProvisioned := cluster.Status.Initialization.InfrastructureProvisioned
	if infraProvisioned == nil || !*infraProvisioned {
		log.Info("Cluster infrastructure is not ready yet")
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.WaitingForClusterInfrastructureReason, "")
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	controllerutil.AddFinalizer(cp, ControlPlaneFinalizer)

	region, vpcID, nodeSubnetID, err := r.clusterNetwork(ctx, cluster, cp)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	// Credentials: identityRef (CCECluster*Identity) takes precedence; when
	// absent, fall back to the per-cluster Secret, then env. The agency from
	// a CCEClusterRoleIdentity is retained and passed to cluster creation
	// (spec.agencyName, when set, still wins).
	creds, identityAgency, err := resolveControlPlaneCredentials(ctx, r.Client, cp)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		recordEvent(r.Recorder, cp, corev1.EventTypeWarning, "CredentialsFailed", "%v", err)
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.CredentialsReadyCondition, "CredentialsResolved", "CCE credentials resolved")

	svc, err := r.newCCEService(region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	// Ensure the CCE cluster exists (idempotent create).
	clusterID := cp.Status.ClusterID
	if clusterID == "" {
		id, err := svc.CreateCluster(ctx, toCreateClusterInput(cp, vpcID, nodeSubnetID, identityAgency))
		if err != nil {
			conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			return resultAfterError(err)
		}
		clusterID = id
		cp.Status.ClusterID = id
		recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterCreated", "created CCE cluster %s", id)
	}

	// Wait for the cluster to become Available, then backfill the endpoint.
	info, err := svc.ShowCluster(ctx, clusterID)
	if err != nil {
		if clouderrors.IsNotFound(err) {
			// Cluster deleted out of band — reset and persist so the next
			// reconcile recreates it (the ID must be cleared in the stored
			// status, otherwise this loops forever).
			cp.Status.ClusterID = ""
			conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
				conditions.ReconciliationFailedReason, "CCE cluster not found, recreating")
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return resultAfterError(err)
	}
	if info.Phase != "Available" {
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationInProgressReason, "CCE cluster phase: "+info.Phase)
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	// Backfill the API server endpoint. Official endpoint type values are
	// "Internal"/"External" (model_cluster_endpoints.go), NOT "public"/
	// "private" — matching on the wrong strings left the endpoint empty.
	for _, ep := range info.Endpoints {
		host, port := splitEndpointURL(ep.URL)
		if port == 0 {
			port = 5443
		}
		endpoint := &clusterv1.APIEndpoint{Host: host, Port: port}
		if ep.Type == "External" || (ep.Type == "Internal" && (cp.Status.ControlPlaneEndpoint == nil || cp.Status.ControlPlaneEndpoint.IsZero())) {
			cp.Status.ControlPlaneEndpoint = endpoint
		}
	}
	cp.Status.Version = info.Version
	conditions.MarkTrue(cp, conditions.CCEClusterReadyCondition, "ClusterAvailable", "CCE cluster is available")
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterAvailable", "CCE cluster %s is available", clusterID)
	conditions.MarkTrue(cp, conditions.CCEClusterReadyCondition, "ClusterAvailable", "CCE cluster is available")

	// Addons reconciliation (declarative set, mirrors CAPA EKS addons): install
	// missing, upgrade version drift, remove those no longer listed.
	if err := r.reconcileAddons(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp, conditions.AddonsConfiguredCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.AddonsConfiguredCondition, "AddonsConfigured", "CCE addons reconciled")

	// Pod-identity associations (declarative set, mirrors EKS Pod Identity).
	if err := r.reconcilePodIdentityAssociations(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp, conditions.PodIdentityAssociationsConfiguredCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.PodIdentityAssociationsConfiguredCondition, "PodIdentityAssociationsConfigured", "CCE pod-identity associations reconciled")

	// Control-plane log collection (mirrors CAPA EKS Logging).
	if err := r.reconcileLogging(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp, conditions.LoggingConfiguredCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.LoggingConfiguredCondition, "LoggingConfigured", "CCE control-plane log config reconciled")

	// CCE access policies (mirrors EKS access entries): declarative set.
	if err := r.reconcileAccessPolicies(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp, conditions.AccessPoliciesConfiguredCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.AccessPoliciesConfiguredCondition, "AccessPoliciesConfigured", "CCE access policies reconciled")

	// Upgrade orchestration (FR-1.7, questionnaire Q11): first poll any
	// in-flight upgrade task; then, when spec.version differs from the running
	// version, drive the CCE upgrade workflow. A missing upgrade path is a
	// normal platform state, not an error (verified live: the platform offers
	// no cross-minor targets from some versions).
	if cp.Status.UpgradeTaskID != "" {
		if res, done := r.pollUpgradeTask(ctx, svc, clusterID, cp); done {
			return res, nil
		}
	}
	if cp.Spec.Version != "" && info.Version != "" && cp.Spec.Version != info.Version {
		if res, done := r.startUpgrade(ctx, svc, clusterID, cp); done {
			return res, nil
		}
	} else {
		conditions.MarkTrue(cp, conditions.UpgradeReadyCondition, "VersionCurrent", "cluster version matches spec")
	}

	// kubeconfig Secret (mirrors the ACK provider kubeconfig contract, so
	// `clusterctl get kubeconfig` works). Refreshed before certificate expiry
	// (questionnaire Q2: duration -1/[1,1827], default 365 days).
	if cp.Status.KubeconfigSecretName == "" || kubeconfigNeedsRefresh(ctx, r.Client, cp.Namespace, cp.Status.KubeconfigSecretName, kubeconfigRefreshThresholdDays) {
		kubeconfig, err := svc.GetClusterKubeconfig(ctx, clusterID, kubeconfigValidityDays)
		if err != nil {
			conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		secretName := cp.Spec.ClusterName + "-kubeconfig"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: cp.Namespace,
				Labels: map[string]string{
					clusterv1.ClusterNameLabel: cluster.Name,
				},
			},
			Data: map[string][]byte{"value": []byte(kubeconfig)},
		}
		// Own the Secret so lifecycle is tied to the control plane (and the
		// delete path can find it). A non-owned pre-existing Secret must not be
		// overwritten silently.
		if err := controllerutil.SetControllerReference(cp, secret, r.Client.Scheme()); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Client.Create(ctx, secret); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason, err.Error())
				return ctrl.Result{}, err
			}
			// Update the existing Secret in place (rotation) — but only if it
			// is the provider's own Secret (owned by this control plane or
			// carrying the cluster label); otherwise refuse to overwrite.
			existing := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: cp.Namespace, Name: secretName}, existing); err != nil {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason, err.Error())
				return ctrl.Result{}, err
			}
			if !metav1.IsControlledBy(existing, cp) && existing.Labels[clusterv1.ClusterNameLabel] != cluster.Name {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason,
					"Secret "+secretName+" exists and is not owned by this provider; refusing to overwrite")
				return ctrl.Result{}, errors.New("refusing to overwrite non-owned kubeconfig Secret " + secretName)
			}
			existing.Data = secret.Data
			if err := r.Update(ctx, existing); err != nil {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason, err.Error())
				return ctrl.Result{}, err
			}
		}
		cp.Status.KubeconfigSecretName = secretName
	}
	conditions.MarkTrue(cp, conditions.KubeconfigReadyCondition, "KubeconfigGenerated", "kubeconfig Secret generated")
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "KubeconfigGenerated", "kubeconfig Secret generated")

	cp.Status.Ready = true
	cp.Status.Initialized = true
	log.Info("CCE control plane is ready", "clusterID", clusterID)
	return ctrl.Result{}, nil
}

// pollUpgradeTask polls an in-flight upgrade task. Returns (result, true) when
// the caller should return immediately; on Success it clears the task and
// continues the normal reconcile (done=false).
func (r *CCEManagedControlPlaneReconciler) pollUpgradeTask(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, bool) {
	log := ctrl.LoggerFrom(ctx)
	phase, err := svc.ShowUpgradeTask(ctx, clusterID, cp.Status.UpgradeTaskID)
	if err != nil {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterForError(err)}, true
	}
	switch phase {
	case cceService.UpgradeTaskPhaseSuccess:
		cp.Status.UpgradeTaskID = ""
		cp.Status.Version = cp.Spec.Version
		conditions.MarkTrue(cp, conditions.UpgradeReadyCondition, "UpgradeCompleted", "cluster upgraded to "+cp.Spec.Version)
		recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "UpgradeCompleted", "cluster upgraded to %s", cp.Spec.Version)
		log.Info("Cluster upgrade completed", "clusterID", clusterID, "version", cp.Spec.Version)
		// Persist and requeue so the NEXT reconcile observes the new version
		// (reconcile's info.Version is stale here and would otherwise re-trigger
		// an upgrade in this same pass).
		return ctrl.Result{RequeueAfter: defaultRequeue}, true
	case cceService.UpgradeTaskPhaseFailed:
		// Clear the task ID: the previous behavior kept it forever and only a
		// manual status edit could unblock; with it cleared, the next reconcile
		// re-evaluates the target (declarative retry). Mark the failure so it
		// is visible in conditions.
		cp.Status.UpgradeTaskID = ""
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, "upgrade task failed")
		return ctrl.Result{RequeueAfter: defaultRequeue}, true
	default: // Init/Queuing/Running/Pause
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.UpgradeInProgressReason, "upgrade task phase: "+phase)
		return ctrl.Result{RequeueAfter: defaultRequeue}, true
	}
}

// startUpgrade decides whether the platform offers the requested target and,
// when it does, starts the upgrade workflow. Returns (result, true) when the
// caller should return immediately.
func (r *CCEManagedControlPlaneReconciler) startUpgrade(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, bool) {
	log := ctrl.LoggerFrom(ctx)
	info, err := svc.GetUpgradeInfo(ctx, clusterID)
	if err != nil {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterForError(err)}, true
	}
	if len(info.TargetVersions) == 0 {
		// Platform currently offers no upgrade target — normal state, not an
		// error (questionnaire Q11, verified live across cluster shapes).
		// Official prerequisite (cce_10_0197): the running patch must be the
		// latest before a version upgrade; when suggestPatch is set, surface
		// it so the user knows to upgrade the patch first.
		msg := "no upgrade targets offered from " + info.CurrentVersion +
			"; check Huawei Cloud upgrade policy"
		if info.SuggestPatch != "" {
			msg += "; upgrade the patch to " + info.SuggestPatch + " first"
		}
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.UpgradeNotOfferedReason, msg)
		return ctrl.Result{RequeueAfter: defaultRequeue}, true
	}
	// The platform returns full target versions (e.g. v1.34.8-r2) while a
	// user may specify a major version (e.g. v1.34) — the official API accepts
	// a major version and resolves the latest patch. Match on the version
	// prefix so a major-version spec is not rejected as unavailable.
	if !containsVersion(info.TargetVersions, cp.Spec.Version) {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.UpgradeTargetUnavailableReason,
			"target version "+cp.Spec.Version+" not offered; available: "+strings.Join(info.TargetVersions, ", "))
		return ctrl.Result{RequeueAfter: defaultRequeue}, true
	}

	taskID, err := svc.StartUpgrade(ctx, clusterID, cp.Spec.Version)
	if err != nil {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterForError(err)}, true
	}
	cp.Status.UpgradeTaskID = taskID
	conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
		conditions.UpgradeInProgressReason, "upgrading to "+cp.Spec.Version)
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "UpgradeStarted", "upgrading to %s", cp.Spec.Version)
	log.Info("Cluster upgrade started", "clusterID", clusterID, "target", cp.Spec.Version, "taskID", taskID)
	return ctrl.Result{RequeueAfter: defaultRequeue}, true
}

func (r *CCEManagedControlPlaneReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	region, _, _, err := r.clusterNetwork(ctx, cluster, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	creds, _, err := resolveControlPlaneCredentials(ctx, r.Client, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	svc, err := r.newCCEService(region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cp.Status.ClusterID != "" {
		if _, err := svc.ShowCluster(ctx, cp.Status.ClusterID); err != nil {
			// Only a 404 means the cluster is already gone. Any transient error
			// (throttle/network) must NOT fall through to removing the
			// finalizer — that would leak the CCE cluster forever.
			if !clouderrors.IsNotFound(err) {
				return ctrl.Result{}, errors.Wrap(err, "failed to check CCE cluster before deletion")
			}
		} else {
			// Delete with explicit options to avoid leftovers (official
			// defaults leave EVS/storage behind — questionnaire Q8).
			if err := svc.DeleteCluster(ctx, cceService.DeleteClusterInput{
				ClusterID:          cp.Status.ClusterID,
				DeleteEVS:          true,
				DeleteENI:          true,
				DeleteELB:          true,
				OnDemandNodePolicy: "delete",
				PeriodicNodePolicy: "reset",
			}); err != nil && !clouderrors.IsNotFound(err) {
				return ctrl.Result{}, errors.Wrap(err, "failed to delete CCE cluster")
			}
			log.Info("CCE cluster deletion requested, waiting", "clusterID", cp.Status.ClusterID)
			recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterDeletionRequested", "deletion requested for CCE cluster %s", cp.Status.ClusterID)
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
	}

	// Delete the kubeconfig Secret.
	if cp.Status.KubeconfigSecretName != "" {
		secret := &corev1.Secret{}
		key := types.NamespacedName{Namespace: cp.Namespace, Name: cp.Status.KubeconfigSecretName}
		if err := r.Get(ctx, key, secret); err == nil {
			if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	controllerutil.RemoveFinalizer(cp, ControlPlaneFinalizer)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *CCEManagedControlPlaneReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1beta1.CCEManagedControlPlane{}).
		WithOptions(opts).
		Named("ccemanagedcontrolplane").
		Complete(r)
}

// ---- helpers ----

// clusterNetwork reads the region and host network (VPC + node subnet) from
// the CCECluster shell (infrastructureRef) — official hostNetwork is required
// at cluster creation (A2, verified by the real CCE smoke test).
func (r *CCEManagedControlPlaneReconciler) clusterNetwork(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane) (region, vpcID, subnetID string, err error) {
	if cluster.Spec.InfrastructureRef.Name == "" {
		return "", "", "", errors.New("cluster has no infrastructureRef")
	}
	cceCluster := &infrav1beta1.CCECluster{}
	key := types.NamespacedName{Namespace: cp.Namespace, Name: cluster.Spec.InfrastructureRef.Name}
	if err := r.Get(ctx, key, cceCluster); err != nil {
		return "", "", "", errors.Wrapf(err, "failed to get CCECluster %s", key)
	}
	if cceCluster.Spec.Region == "" {
		return "", "", "", errors.New("CCECluster spec.region is empty")
	}
	vpcID = cceCluster.Spec.Network.VPC.ID
	if len(cceCluster.Spec.Network.Subnets) > 0 {
		subnetID = cceCluster.Spec.Network.Subnets[0].ID
	}
	return cceCluster.Spec.Region, vpcID, subnetID, nil
}

// toCreateClusterInput maps the control plane spec to the CreateCluster
// input. identityAgency (from a CCEClusterRoleIdentity, when one is
// referenced) fills the cluster agency when the spec does not set it
// explicitly - an explicit spec.agencyName always wins.
func toCreateClusterInput(cp *controlplanev1beta1.CCEManagedControlPlane, vpcID, nodeSubnetID, identityAgency string) cceService.CreateClusterInput {
	agency := cp.Spec.AgencyName
	if agency == "" {
		agency = identityAgency
	}
	return cceService.CreateClusterInput{
		Name:                 cp.Spec.ClusterName,
		Category:             cp.Spec.Category,
		Flavor:               cp.Spec.Flavor,
		Version:              cp.Spec.Version,
		ContainerNetworkMode: cp.Spec.ContainerNetwork.Mode,
		ContainerNetworkCIDR: cp.Spec.ContainerNetwork.CIDR,
		ENISubnets:           cp.Spec.ContainerNetwork.ENISubnets,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  nodeSubnetID,
		ServiceCIDR:          cp.Spec.ServiceNetwork.CIDR,
		CustomSAN:            cp.Spec.CustomSan,
		PublicAccess:         cp.Spec.EndpointAccess.Public,
		PublicAccessCIDRs:    cp.Spec.EndpointAccess.CIDRs,
		AgencyName:           agency,
		BillingMode:          cp.Spec.Billing.Mode,
	}
}

// containsVersion reports whether targets contains a version matching the
// requested one: exact match, or the requested version as a prefix of a full
// target (e.g. "v1.34" matches "v1.34.8-r2").
func containsVersion(targets []string, requested string) bool {
	for _, t := range targets {
		if t == requested || strings.HasPrefix(t, requested+".") {
			return true
		}
	}
	return false
}

// splitEndpointURL parses a CCE endpoint URL (https://10.0.0.10:5443) into
// host and port. Port 0 is returned when absent (callers then default it).
func splitEndpointURL(raw string) (string, int32) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0
	}
	host := u.Hostname()
	if host == "" {
		host = u.Host
	}
	port := int32(0)
	if p, err := strconv.Atoi(u.Port()); err == nil {
		port = int32(p)
	}
	return host, port
}

// reconcileAddons reconciles the declared addon set against the cloud: create
// missing, upgrade version drift, delete those no longer listed.
func (r *CCEManagedControlPlaneReconciler) reconcileAddons(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta1.CCEManagedControlPlane) error {
	if len(cp.Spec.Addons) == 0 {
		return nil
	}
	current, err := svc.ListAddonInstances(ctx, clusterID)
	if err != nil {
		return err
	}
	cloudByName := map[string]cceService.AddonInfo{}
	for _, a := range current {
		cloudByName[a.Name] = a
	}
	specByName := map[string]controlplanev1beta1.AddonSpec{}
	for _, a := range cp.Spec.Addons {
		specByName[a.Name] = a
	}

	// Create missing / upgrade drift.
	for _, want := range cp.Spec.Addons {
		got, exists := cloudByName[want.Name]
		switch {
		case !exists:
			if _, err := svc.CreateAddonInstance(ctx, cceService.AddonInput{
				ClusterID: clusterID, Name: want.Name, Version: want.Version,
			}); err != nil {
				return err
			}
		case want.Version != "" && want.Version != got.Version:
			if err := svc.UpdateAddonInstance(ctx, cceService.AddonInput{
				ClusterID: clusterID, AddonID: got.ID, Name: want.Name, Version: want.Version,
			}); err != nil {
				return err
			}
		}
	}
	// Remove addons no longer listed.
	for _, got := range current {
		if _, keep := specByName[got.Name]; !keep {
			if err := svc.DeleteAddonInstance(ctx, clusterID, got.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconcilePodIdentityAssociations reconciles the declared pod-identity
// associations against the cloud: create missing, delete removed.
func (r *CCEManagedControlPlaneReconciler) reconcilePodIdentityAssociations(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta1.CCEManagedControlPlane) error {
	if len(cp.Spec.PodIdentityAssociations) == 0 {
		return nil
	}
	current, err := svc.ListPodIdentityAssociations(ctx, clusterID)
	if err != nil {
		return err
	}
	key := func(ns, sa string) string { return ns + "/" + sa }
	cloudByKey := map[string]cceService.PodIdentityAssociationInfo{}
	for _, a := range current {
		cloudByKey[key(a.Namespace, a.ServiceAccount)] = a
	}
	specByKey := map[string]controlplanev1beta1.PodIdentityAssociationSpec{}
	for _, a := range cp.Spec.PodIdentityAssociations {
		specByKey[key(a.Namespace, a.ServiceAccount)] = a
	}

	// Create missing.
	for _, want := range cp.Spec.PodIdentityAssociations {
		k := key(want.Namespace, want.ServiceAccount)
		if _, exists := cloudByKey[k]; !exists {
			if _, err := svc.CreatePodIdentityAssociation(ctx, cceService.PodIdentityAssociationInput{
				ClusterID:      clusterID,
				Namespace:      want.Namespace,
				ServiceAccount: want.ServiceAccount,
				AgencyName:     want.AgencyName,
			}); err != nil {
				return err
			}
		}
	}
	// Delete removed.
	for _, got := range current {
		if _, keep := specByKey[key(got.Namespace, got.ServiceAccount)]; !keep {
			if err := svc.DeletePodIdentityAssociation(ctx, clusterID, got.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconcileLogging reconciles the declared control-plane log collection config
// against the cloud (mirrors CAPA EKS Logging). Declarative: TTL + the exact
// log item set, compared against ShowClusterConfig, applied via
// UpdateClusterLogConfig on drift.
func (r *CCEManagedControlPlaneReconciler) reconcileLogging(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta1.CCEManagedControlPlane) error {
	if cp.Spec.Logging == nil {
		return nil
	}
	want := cceService.LogConfigInfo{
		TTLInDays: cp.Spec.Logging.TTLInDays,
		Logs:      make([]cceService.LogConfigInput, 0, len(cp.Spec.Logging.Logs)),
	}
	for _, l := range cp.Spec.Logging.Logs {
		want.Logs = append(want.Logs, cceService.LogConfigInput{Name: l.Name, Type: l.Type, Enable: l.Enable})
	}
	got, err := svc.ShowClusterLogConfig(ctx, clusterID)
	if err != nil {
		return err
	}
	if logConfigEqual(got, &want) {
		return nil
	}
	return svc.UpdateClusterLogConfig(ctx, clusterID, want.TTLInDays, want.Logs)
}

// logConfigEqual reports whether two log configs match (order-insensitive on
// the log item set; empty type on either side is treated as "control").
func logConfigEqual(a, b *cceService.LogConfigInfo) bool {
	if a.TTLInDays != b.TTLInDays {
		return false
	}
	if len(a.Logs) != len(b.Logs) {
		return false
	}
	key := func(l cceService.LogConfigInput) string {
		t := l.Type
		if t == "" {
			t = "control"
		}
		return l.Name + "/" + t + "/" + strconv.FormatBool(l.Enable)
	}
	set := map[string]int{}
	for _, l := range a.Logs {
		set[key(l)]++
	}
	for _, l := range b.Logs {
		if set[key(l)] == 0 {
			return false
		}
		set[key(l)]--
	}
	return true
}

// reconcileAccessPolicies reconciles the declared CCE access policies against
// the account: create missing (by name), update drift (policyType/principal/
// namespaces), delete those no longer listed. CCE access policies are account-
// scoped (one policy may span many clusters), so they are keyed by name and
// scoped to the owning cluster via clusters=[clusterID].
func (r *CCEManagedControlPlaneReconciler) reconcileAccessPolicies(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta1.CCEManagedControlPlane) error {
	if len(cp.Spec.AccessPolicies) == 0 {
		return nil
	}
	current, err := svc.ListAccessPolicies(ctx)
	if err != nil {
		return err
	}
	cloudByName := map[string]cceService.AccessPolicyInfo{}
	for _, p := range current {
		cloudByName[p.Name] = p
	}
	specByName := map[string]controlplanev1beta1.AccessPolicySpec{}
	for _, p := range cp.Spec.AccessPolicies {
		specByName[p.Name] = p
	}

	// Create missing / update drift.
	for _, want := range cp.Spec.AccessPolicies {
		input := toAccessPolicyInput(clusterID, want)
		got, exists := cloudByName[want.Name]
		switch {
		case !exists:
			if _, err := svc.CreateAccessPolicy(ctx, input); err != nil {
				return err
			}
		case accessPolicyDrifted(got, want):
			if err := svc.UpdateAccessPolicy(ctx, got.PolicyID, input); err != nil {
				return err
			}
		}
	}
	// Remove policies no longer listed.
	for _, got := range current {
		if _, keep := specByName[got.Name]; !keep {
			if err := svc.DeleteAccessPolicy(ctx, got.PolicyID); err != nil {
				return err
			}
		}
	}
	return nil
}

// toAccessPolicyInput maps a spec to the service input, scoping the policy to
// the owning cluster.
func toAccessPolicyInput(clusterID string, p controlplanev1beta1.AccessPolicySpec) cceService.AccessPolicyInput {
	return cceService.AccessPolicyInput{
		Name:          p.Name,
		ClusterID:     clusterID,
		PolicyType:    p.PolicyType,
		PrincipalType: p.PrincipalType,
		PrincipalIDs:  p.PrincipalIds,
		Namespaces:    p.Namespaces,
	}
}

// accessPolicyDrifted reports whether a cloud access policy differs from the
// declared spec (empty spec namespaces default to ["*"]).
func accessPolicyDrifted(got cceService.AccessPolicyInfo, want controlplanev1beta1.AccessPolicySpec) bool {
	if got.PolicyType != want.PolicyType || got.PrincipalType != want.PrincipalType {
		return true
	}
	if !slices.Equal(got.PrincipalIDs, want.PrincipalIds) {
		return true
	}
	wantNS := want.Namespaces
	if len(wantNS) == 0 {
		wantNS = []string{"*"}
	}
	if !slices.Equal(got.Namespaces, wantNS) {
		return true
	}
	return false
}
