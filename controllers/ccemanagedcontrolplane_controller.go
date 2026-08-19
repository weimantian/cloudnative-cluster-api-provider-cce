/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func (r *CCEManagedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	cp := &controlplanev1beta1.CCEManagedControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

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
		if err := r.Status().Update(ctx, cp); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	if controllerutil.AddFinalizer(cp, ControlPlaneFinalizer) {
		if err := r.Update(ctx, cp); err != nil {
			return ctrl.Result{}, err
		}
	}

	region, vpcID, nodeSubnetID, err := r.clusterNetwork(ctx, cluster, cp)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}

	// Credentials: per-cluster Secret (<cluster>-credentials) with env fallback.
	creds, err := scope.ResolveCredentials(ctx, r.Client, cp.Namespace, cp.Spec.ClusterName+"-credentials")
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.CredentialsReadyCondition, "CredentialsResolved", "CCE credentials resolved")

	svc, err := r.newCCEService(region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return ctrl.Result{}, err
	}

	// Ensure the CCE cluster exists (idempotent create).
	clusterID := cp.Status.ClusterID
	if clusterID == "" {
		id, err := svc.CreateCluster(ctx, toCreateClusterInput(cp, vpcID, nodeSubnetID))
		if err != nil {
			conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			if uerr := r.Status().Update(ctx, cp); uerr != nil {
				return ctrl.Result{}, uerr
			}
			return resultAfterError(err)
		}
		clusterID = id
		cp.Status.ClusterID = id
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
			if uerr := r.Status().Update(ctx, cp); uerr != nil {
				return ctrl.Result{}, uerr
			}
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, uerr
		}
		return resultAfterError(err)
	}
	if info.Phase != "Available" {
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationInProgressReason, "CCE cluster phase: "+info.Phase)
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, uerr
		}
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
			if uerr := r.Status().Update(ctx, cp); uerr != nil {
				return ctrl.Result{}, uerr
			}
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
				if uerr := r.Status().Update(ctx, cp); uerr != nil {
					return ctrl.Result{}, uerr
				}
				return ctrl.Result{}, err
			}
			// Update the existing Secret in place (rotation) — but only if it
			// is the provider's own Secret (owned by this control plane or
			// carrying the cluster label); otherwise refuse to overwrite.
			existing := &corev1.Secret{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: cp.Namespace, Name: secretName}, existing); err != nil {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason, err.Error())
				if uerr := r.Status().Update(ctx, cp); uerr != nil {
					return ctrl.Result{}, uerr
				}
				return ctrl.Result{}, err
			}
			if !metav1.IsControlledBy(existing, cp) && existing.Labels[clusterv1.ClusterNameLabel] != cluster.Name {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason,
					"Secret "+secretName+" exists and is not owned by this provider; refusing to overwrite")
				if uerr := r.Status().Update(ctx, cp); uerr != nil {
					return ctrl.Result{}, uerr
				}
				return ctrl.Result{}, errors.New("refusing to overwrite non-owned kubeconfig Secret " + secretName)
			}
			existing.Data = secret.Data
			if err := r.Update(ctx, existing); err != nil {
				conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
					conditions.ReconciliationFailedReason, err.Error())
				if uerr := r.Status().Update(ctx, cp); uerr != nil {
					return ctrl.Result{}, uerr
				}
				return ctrl.Result{}, err
			}
		}
		cp.Status.KubeconfigSecretName = secretName
	}
	conditions.MarkTrue(cp, conditions.KubeconfigReadyCondition, "KubeconfigGenerated", "kubeconfig Secret generated")

	cp.Status.Ready = true
	cp.Status.Initialized = true
	log.Info("CCE control plane is ready", "clusterID", clusterID)
	// Persist status explicitly (status subresource ignores r.Update).
	if err := r.Status().Update(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
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
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, true
		}
		return ctrl.Result{RequeueAfter: requeueAfterForError(err)}, true
	}
	switch phase {
	case cceService.UpgradeTaskPhaseSuccess:
		cp.Status.UpgradeTaskID = ""
		cp.Status.Version = cp.Spec.Version
		conditions.MarkTrue(cp, conditions.UpgradeReadyCondition, "UpgradeCompleted", "cluster upgraded to "+cp.Spec.Version)
		log.Info("Cluster upgrade completed", "clusterID", clusterID, "version", cp.Spec.Version)
		// Persist and requeue so the NEXT reconcile observes the new version
		// (reconcile's info.Version is stale here and would otherwise re-trigger
		// an upgrade in this same pass).
		return r.persistUpgradeStatus(ctx, cp)
	case cceService.UpgradeTaskPhaseFailed:
		// Clear the task ID: the previous behavior kept it forever and only a
		// manual status edit could unblock; with it cleared, the next reconcile
		// re-evaluates the target (declarative retry). Mark the failure so it
		// is visible in conditions.
		cp.Status.UpgradeTaskID = ""
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, "upgrade task failed")
		return r.persistUpgradeStatus(ctx, cp)
	default: // Init/Queuing/Running/Pause
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.UpgradeInProgressReason, "upgrade task phase: "+phase)
		return r.persistUpgradeStatus(ctx, cp)
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
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, true
		}
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
		return r.persistUpgradeStatus(ctx, cp)
	}
	// The platform returns full target versions (e.g. v1.34.8-r2) while a
	// user may specify a major version (e.g. v1.34) — the official API accepts
	// a major version and resolves the latest patch. Match on the version
	// prefix so a major-version spec is not rejected as unavailable.
	if !containsVersion(info.TargetVersions, cp.Spec.Version) {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.UpgradeTargetUnavailableReason,
			"target version "+cp.Spec.Version+" not offered; available: "+strings.Join(info.TargetVersions, ", "))
		return r.persistUpgradeStatus(ctx, cp)
	}

	taskID, err := svc.StartUpgrade(ctx, clusterID, cp.Spec.Version)
	if err != nil {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		if uerr := r.Status().Update(ctx, cp); uerr != nil {
			return ctrl.Result{}, true
		}
		return ctrl.Result{RequeueAfter: requeueAfterForError(err)}, true
	}
	cp.Status.UpgradeTaskID = taskID
	conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
		conditions.UpgradeInProgressReason, "upgrading to "+cp.Spec.Version)
	log.Info("Cluster upgrade started", "clusterID", clusterID, "target", cp.Spec.Version, "taskID", taskID)
	return r.persistUpgradeStatus(ctx, cp)
}

// persistUpgradeStatus stores the control plane status after an upgrade step
// and requests a requeue so the in-flight task keeps being polled. A status
// update failure is logged and requeued (the reconcile loop will retry).
func (r *CCEManagedControlPlaneReconciler) persistUpgradeStatus(ctx context.Context, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, bool) {
	if err := r.Status().Update(ctx, cp); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to persist upgrade status")
		return ctrl.Result{RequeueAfter: defaultRequeue}, true
	}
	return ctrl.Result{RequeueAfter: defaultRequeue}, true
}

func (r *CCEManagedControlPlaneReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	region, _, _, err := r.clusterNetwork(ctx, cluster, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	creds, err := scope.ResolveCredentials(ctx, r.Client, cp.Namespace, cp.Spec.ClusterName+"-credentials")
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
	if err := r.Update(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *CCEManagedControlPlaneReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1beta1.CCEManagedControlPlane{}).
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

func toCreateClusterInput(cp *controlplanev1beta1.CCEManagedControlPlane, vpcID, nodeSubnetID string) cceService.CreateClusterInput {
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
		AgencyName:           cp.Spec.AgencyName,
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
