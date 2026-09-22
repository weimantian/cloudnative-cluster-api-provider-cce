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
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
	iamService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/iam"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/tags"
)

// ControlPlaneFinalizer ensures the CCE cluster is deleted before the object.
const ControlPlaneFinalizer = "ccemanagedcontrolplane.controlplane.cluster.x-k8s.io"

// kubeconfigValidityDays is the requested certificate validity (questionnaire
// Q2: -1 or [1,1827]; 365 = one year).
const kubeconfigValidityDays = 365

// reconciliationPeriod is the steady-state requeue interval: it turns the
// drift-sync (ReconcileClusterTags) and other reconciliation into a periodic
// sweep so external changes on the cloud side (e.g. a tag edited in the
// console) are detected and pulled back to the declared spec without
// requiring a CR event.
const reconciliationPeriod = 10 * time.Minute

// credentialsSecretSuffix is the suffix of the per-cluster credentials Secret
// (<clusterName>-credentials) that carries the AK/SK used by the provider to
// drive the CCE API. The controller watches it so rotating the Secret takes
// effect without restarting the provider.
const credentialsSecretSuffix = "-credentials"

// controlPlaneDeletePollAnnotation is stamped (minute granularity) while a CCE
// cluster deletion is in flight. It doubles as (1) the record that DeleteCluster
// was already requested — so the call is not re-issued on every poll — and (2) a
// keep-alive that makes the informer watch re-drive the reconcile even when the
// workqueue dedup coalesces the delayed requeue away while the object is
// terminating (observed live on the node-pool delete path).
const controlPlaneDeletePollAnnotation = "controlplane.cluster.x-k8s.io/last-cluster-delete-poll"

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
	ServiceFactory func(regionID string, creds *credentials.Credentials) (cceService.Service, error)

	// IAMServiceFactory builds the IAM trust-agency service for a region/
	// credential pair. Overridden in tests with a fake; defaults to
	// iamService.NewClient (see newIAMService).
	IAMServiceFactory func(regionID string, creds *credentials.Credentials) (iamService.Service, error)

	// CredentialProvider resolves temporary security credentials for an
	// agency-based identity. Nil means agency identities cannot be assumed
	// (static AK/SK only). Injected in SetupControllers; nil in tests.
	CredentialProvider credentials.Provider
}

// newCCEService returns a CCE service via the injected factory, or the real
// implementation when no factory is set.
func (r *CCEManagedControlPlaneReconciler) newCCEService(regionID string, creds *credentials.Credentials) (cceService.Service, error) {
	if r.ServiceFactory != nil {
		return r.ServiceFactory(regionID, creds)
	}
	return cceService.NewClient(regionID, creds)
}

// newIAMService returns an IAM service via the injected factory, or the real
// implementation when no factory is set.
func (r *CCEManagedControlPlaneReconciler) newIAMService(regionID string, creds *credentials.Credentials) (iamService.Service, error) {
	if r.IAMServiceFactory != nil {
		return r.IAMServiceFactory(regionID, creds)
	}
	return iamService.NewClient(regionID, creds)
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cceclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile implements the reconcile loop of CCEManagedControlPlane. Uses
// the per-reconcile CCEManagedControlPlaneScope to hold the patchHelper,
// CR references. The
// scope's PatchObject() (called via defer) atomically updates
// status.observedGeneration via patch.WithStatusObservedGeneration.
func (r *CCEManagedControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)
	// A tuned backoff result (throttle/quota/permission) is deliberately returned
	// with a nil error so controller-runtime does not override the delay, and it
	// increments the failure counter below — that must not be mistaken for a clean
	// reconcile. Any reconcile that completes without an error and without such a
	// backoff resets the counter, so a stale capped counter from an earlier burst
	// does not make the next genuine transient failure wait the full backoffMax.
	// (The steady-state success returns RequeueAfter: reconciliationPeriod, so
	// gating the reset on RequeueAfter==0 never fired.)
	failuresBefore := errorBackoff.failures(req.NamespacedName)
	defer func() {
		if reterr == nil && errorBackoff.failures(req.NamespacedName) == failuresBefore {
			resetBackoff(req.NamespacedName)
		}
	}()

	cp := &controlplanev1beta2.CCEManagedControlPlane{}
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

	// Branch to reconcileDelete before constructing the scope (matches the
	// pre-refactor structure: scope is only needed for normal reconcile
	// since the delete path doesn't write status.observedGeneration).
	if !cp.ObjectMeta.DeletionTimestamp.IsZero() {
		if cluster == nil {
			// Deleting without an owner Cluster reference: reconcileDelete
			// dereferences cluster.Spec.InfrastructureRef and would panic, so
			// never call it with a nil cluster. Requeue (do not return an
			// empty Result) — otherwise the finalizer is stranded with no
			// retry. The owner ref is a CAPI-managed invariant set once, so a
			// slow interval is enough.
			log.Info("Deleting control plane has no owner Cluster reference, deferring deletion")
			return ctrl.Result{RequeueAfter: reconciliationPeriod}, nil
		}
		res, err := r.reconcileDelete(ctx, cluster, cp)
		return res, err
	}
	if cluster == nil {
		log.Info("Cluster controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	// Build the per-reconcile scope (constructor builds the patchHelper
	// and snapshots Status.ObservedGeneration for coalesced-event detection).
	scope, err := scope.NewCCEManagedControlPlaneScope(scope.CCEManagedControlPlaneScopeParams{
		Client:                 r.Client,
		Cluster:                cluster,
		CCEManagedControlPlane: cp,
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to build CCM scope")
	}
	defer func() {
		if err := scope.Close(ctx); err != nil && reterr == nil {
			reterr = err
		}
	}()
	res, err = r.reconcileNormal(ctx, cluster, cp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Requeue when observed generation is behind current generation.
	// Catches spec changes coalesced into the in-flight work queue entry
	// (event coalescing would otherwise silently drop them). Do NOT override a
	// tuned backoff (throttle/quota via the failure counter, permission via its
	// fixed delay): shortening it to defaultRequeue would put the next attempt
	// straight back into the rate-limit window the backoff exists to escape.
	classifiedBackoff := errorBackoff.failures(req.NamespacedName) != failuresBefore ||
		res.RequeueAfter == permissionBackoff
	if !classifiedBackoff && scope.ObservedGenerationAtStart() < scope.GenerationAtStart() {
		log.Info("Observed generation behind current generation, requeueing",
			"observedGeneration", scope.ObservedGenerationAtStart(),
			"generation", scope.GenerationAtStart())
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}
	return res, nil
}

func (r *CCEManagedControlPlaneReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta2.CCEManagedControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Identity invariant: spec.clusterName names both the cloud CCE cluster and
	// the ownership tag key (cluster-api-provider-cce.cluster.<clusterName>=owned).
	// ExternalResourceGC matches that key against Cluster CR names, so a
	// divergent name makes the sweeper treat the live CCE cluster as an orphan
	// and delete it. Enforce clusterName == owning Cluster name before any cloud
	// call. clusterName is immutable, so this state never self-heals: delete and
	// recreate the object with the correct name.
	if cp.Spec.ClusterName != cluster.Name {
		msg := errors.Errorf("spec.clusterName %q must equal the owning Cluster name %q",
			cp.Spec.ClusterName, cluster.Name).Error()
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.CCEClusterNameMismatchReason, msg)
		recordEvent(r.Recorder, cp, corev1.EventTypeWarning,
			"InvalidClusterName", "%s", msg)
		return ctrl.Result{}, nil
	}

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

	region, vpcID, nodeSubnetID, eniSubnets, err := r.clusterNetwork(ctx, cluster, cp)
	if err != nil {
		conditions.MarkFalse(cp,
			conditions.CCEClusterReadyCondition,
			conditions.CCEClusterNotFoundReason, err.Error())
		return ctrl.Result{}, err
	}

	// Credentials: identityRef (CCECluster*Identity) takes precedence; when
	// absent, fall back to the per-cluster Secret, then env. The agency from
	// a CCEClusterRoleIdentity is retained and passed to cluster creation
	// (spec.agencyName, when set, still wins).
	creds, identityAgency, err := resolveControlPlaneCredentials(ctx, r.Client, cp)
	if err != nil {
		conditions.MarkFalse(cp,
			conditions.CredentialsReadyCondition,
			conditions.CredentialsResolutionFailedReason, err.Error())
		recordEvent(r.Recorder, cp, corev1.EventTypeWarning, "CredentialsFailed", "%v", err)
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.CredentialsReadyCondition, "CredentialsResolved", "CCE credentials resolved")

	// P1-3 IAM trust-agency auto-creation: when the role identity carries an
	// agency AND the spec declares a v5 trust policy, ensure the agency exists
	// (List -> Create when absent) before assuming it via STS. Creation must use
	// static AK/SK — it cannot assume the very agency it is about to create.
	if cp.Spec.AgencyTrustPolicy != "" && identityAgency != "" {
		if err := iamService.ValidateTrustPolicy(cp.Spec.AgencyTrustPolicy); err != nil {
			conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
				conditions.AgencyCreationFailedReason, err.Error())
			recordEvent(r.Recorder, cp, corev1.EventTypeWarning, "AgencyCreationFailed", "%v", err)
			return ctrl.Result{}, err
		}
		staticCreds := &credentials.Credentials{AccessKey: creds.AccessKey, SecretKey: creds.SecretKey}
		iamSvc, err := r.newIAMService(region, staticCreds)
		if err != nil {
			conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
				conditions.AgencyCreationFailedReason, err.Error())
			recordEvent(r.Recorder, cp, corev1.EventTypeWarning, "AgencyCreationFailed", "%v", err)
			return ctrl.Result{}, err
		}
		if err := iamSvc.EnsureAgency(ctx, identityAgency, cp.Spec.AgencyTrustPolicy); err != nil {
			conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
				conditions.AgencyCreationFailedReason, err.Error())
			recordEvent(r.Recorder, cp, corev1.EventTypeWarning, "AgencyCreationFailed", "%v", err)
			return ctrl.Result{}, err
		}
	}

	resolved, err := credentials.Resolve(ctx, r.CredentialProvider, region, identityAgency, creds.AccessKey, creds.SecretKey)
	if err != nil {
		conditions.MarkFalse(cp,
			conditions.CredentialsReadyCondition,
			conditions.CredentialsResolutionFailedReason, err.Error())
		recordEvent(r.Recorder, cp, corev1.EventTypeWarning, "CredentialsFailed", "%v", err)
		return ctrl.Result{}, err
	}
	svc, err := r.newCCEService(region, resolved)
	if err != nil {
		conditions.MarkFalse(cp,
			conditions.CredentialsReadyCondition,
			conditions.CredentialsResolutionFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	// Ensure the CCE cluster exists (idempotent create).
	clusterID := cp.Status.ClusterID
	if clusterID == "" {
		// ENI (Turbo) container subnets: the control-plane spec wins; when
		// empty, fall back to the managed ENI subnets recorded on the
		// CCECluster network spec (neutron_subnet_id).
		eni := cp.Spec.ContainerNetwork.ENISubnets
		if len(eni) == 0 {
			eni = eniSubnets
		}
		id, err := svc.CreateCluster(ctx, toCreateClusterInput(cp, vpcID, nodeSubnetID, identityAgency, eni))
		if err != nil {
			conditions.MarkFalse(cp,
				conditions.CCEClusterReadyCondition,
				conditions.CCEClusterNotFoundReason, err.Error())
			return resultAfterError(client.ObjectKeyFromObject(cp), err)
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
			conditions.MarkFalse(cp,
				conditions.CCEClusterReadyCondition,
				conditions.CCEClusterNotFoundReason, "CCE cluster not found, recreating")
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
		conditions.MarkFalse(cp,
			conditions.CCEClusterReadyCondition,
			conditions.CCEClusterNotFoundReason, err.Error())
		return resultAfterError(client.ObjectKeyFromObject(cp), err)
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
			// Backfill the spec endpoint too: CAPI's control-plane contract reads
			// spec.controlPlaneEndpoint (not status) to populate
			// Cluster.spec.controlPlaneEndpoint, which gates the Provisioned phase.
			cp.Spec.ControlPlaneEndpoint = endpoint
		}
	}
	conditions.MarkTrue(cp, conditions.CCEClusterReadyCondition, "ClusterAvailable", "CCE cluster is available")
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterAvailable", "CCE cluster %s is available", clusterID)

	// Public API-server access: spec.endpointAccess.public only makes CCE apply
	// the publicAccess whitelist — the EIP itself must be created and bound via
	// UpdateClusterEip. Bind it once, persist the id, then requeue so the next
	// pass reads the new External endpoint into status (the kubeconfig then uses
	// it). The EIP id is released by the delete path.
	if cp.Spec.EndpointAccess.Public && cp.Status.ControlPlaneEIPID == "" {
		eipID, _, berr := svc.BindClusterEip(ctx, clusterID, cp.Spec.ClusterName)
		if berr != nil {
			conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
				conditions.CCEClusterNotFoundReason, berr.Error())
			return resultAfterError(client.ObjectKeyFromObject(cp), berr)
		}
		cp.Status.ControlPlaneEIPID = eipID
		recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterEipBound", "bound public EIP %s to the CCE API server", eipID)
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterAvailable", "CCE cluster %s is available", clusterID)

	// Tag drift sync (FR-1.9): reconcile spec.additionalTags
	// against the cloud cluster tags so declarations stay authoritative even
	// after creation (add/update drifted tags, remove extras, never the owned
	// tag). Failures requeue without flipping the readiness condition.
	if err := svc.ReconcileClusterTags(ctx, clusterID, cp.Spec.ClusterName, cp.Spec.AdditionalTags); err != nil {
		// Post-Available write: use the tuned backoff instead of letting
		// controller-runtime's fast exponential backoff hammer the write API.
		return resultAfterError(client.ObjectKeyFromObject(cp), err)
	}

	// Addons reconciliation (declarative set): install
	// missing, upgrade version drift, remove those no longer listed.
	if err := r.reconcileAddons(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp,
			conditions.AddonsConfiguredCondition,
			conditions.AddonInstallFailedReason, err.Error())
		return resultAfterError(client.ObjectKeyFromObject(cp), err)
	}
	conditions.MarkTrue(cp, conditions.AddonsConfiguredCondition, "AddonsConfigured", "CCE addons reconciled")

	// Pod-identity associations (declarative set, mirrors Pod Identity).
	if err := r.reconcilePodIdentityAssociations(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp,
			conditions.PodIdentityAssociationsConfiguredCondition,
			conditions.PodIdentityCreationFailedReason, err.Error())
		return resultAfterError(client.ObjectKeyFromObject(cp), err)
	}
	conditions.MarkTrue(cp, conditions.PodIdentityAssociationsConfiguredCondition, "PodIdentityAssociationsConfigured", "CCE pod-identity associations reconciled")

	// Control-plane log collection.
	if err := r.reconcileLogging(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp,
			conditions.LoggingConfiguredCondition,
			conditions.LogConfigUpdateFailedReason, err.Error())
		return resultAfterError(client.ObjectKeyFromObject(cp), err)
	}
	conditions.MarkTrue(cp, conditions.LoggingConfiguredCondition, "LoggingConfigured", "CCE control-plane log config reconciled")

	// CCE access policies (mirrors access entries): declarative set.
	if err := r.reconcileAccessPolicies(ctx, svc, clusterID, cp); err != nil {
		conditions.MarkFalse(cp,
			conditions.AccessPoliciesConfiguredCondition,
			conditions.AccessPolicyCreateFailedReason, err.Error())
		return resultAfterError(client.ObjectKeyFromObject(cp), err)
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
	if cp.Spec.Version != "" && info.Version != "" && !sameMajorMinor(cp.Spec.Version, info.Version) {
		if res, done := r.startUpgrade(ctx, svc, clusterID, cp); done {
			return res, nil
		}
	} else {
		conditions.MarkTrue(cp, conditions.UpgradeReadyCondition, "VersionCurrent", "cluster version matches spec")
	}

	// kubeconfig Secrets (so
	// `clusterctl get kubeconfig` works). The CAPI secret is refreshed before
	// certificate expiry;
	// a second user secret (<cluster>-user-kubeconfig) gives users
	// an independent credential. Both are owned by the control plane so they
	// are cleaned up on delete.
	if err := r.ensureKubeconfigSecret(ctx, cp, cluster, svc, clusterID, cp.Spec.ClusterName+"-kubeconfig", kubeconfigValidityDays); err != nil {
		conditions.MarkFalse(cp,
			conditions.KubeconfigReadyCondition,
			conditions.KubeconfigGenerationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	cp.Status.KubeconfigSecretName = cp.Spec.ClusterName + "-kubeconfig"
	if err := r.ensureKubeconfigSecret(ctx, cp, cluster, svc, clusterID, cp.Spec.ClusterName+"-user-kubeconfig", kubeconfigValidityDays); err != nil {
		conditions.MarkFalse(cp,
			conditions.KubeconfigReadyCondition,
			conditions.KubeconfigGenerationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.KubeconfigReadyCondition, "KubeconfigGenerated", "kubeconfig Secrets generated")
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "KubeconfigGenerated", "kubeconfig Secrets generated")

	cp.Status.Ready = true
	cp.Status.Initialized = true
	cp.Status.Initialization.ControlPlaneInitialized = true
	log.Info("CCE control plane is ready", "clusterID", clusterID)
	// Periodic requeue: sweep cloud drift (tags, kubeconfig expiry, ...) even
	// when nothing changed on the CR side.
	return ctrl.Result{RequeueAfter: reconciliationPeriod}, nil
}

// pollUpgradeTask polls an in-flight upgrade task. Returns (result, true) when
// the caller should return immediately; on Success it clears the task and
// continues the normal reconcile (done=false).
func (r *CCEManagedControlPlaneReconciler) pollUpgradeTask(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta2.CCEManagedControlPlane) (ctrl.Result, bool) {
	log := ctrl.LoggerFrom(ctx)
	phase, err := svc.ShowUpgradeTask(ctx, clusterID, cp.Status.UpgradeTaskID)
	if err != nil {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterForError(client.ObjectKeyFromObject(cp), err)}, true
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
func (r *CCEManagedControlPlaneReconciler) startUpgrade(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta2.CCEManagedControlPlane) (ctrl.Result, bool) {
	log := ctrl.LoggerFrom(ctx)
	info, err := svc.GetUpgradeInfo(ctx, clusterID)
	if err != nil {
		conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{RequeueAfter: requeueAfterForError(client.ObjectKeyFromObject(cp), err)}, true
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
		return ctrl.Result{RequeueAfter: requeueAfterForError(client.ObjectKeyFromObject(cp), err)}, true
	}
	cp.Status.UpgradeTaskID = taskID
	conditions.MarkFalse(cp, conditions.UpgradeReadyCondition,
		conditions.UpgradeInProgressReason, "upgrading to "+cp.Spec.Version)
	recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "UpgradeStarted", "upgrading to %s", cp.Spec.Version)
	log.Info("Cluster upgrade started", "clusterID", clusterID, "target", cp.Spec.Version, "taskID", taskID)
	return ctrl.Result{RequeueAfter: defaultRequeue}, true
}

// sameMajorMinor reports whether two Kubernetes version strings share the same
// major.minor prefix (e.g. "v1.35.0" and "v1.35.5" are both v1.35). CCE picks
// the latest patch itself, so a spec version like "v1.35.0" and the running
// "v1.35.5" must be treated as the same version — otherwise the reconciler
// endlessly re-triggers an upgrade (which has no target from the current patch)
// and never marks the control plane Ready, blocking node pool creation.
func sameMajorMinor(a, b string) bool {
	return majorMinor(a) == majorMinor(b)
}

// majorMinor returns the "vMAJOR.MINOR" prefix of a Kubernetes version string,
// or the input unchanged when it does not have a MAJOR.MINOR(.PATCH) shape.
func majorMinor(v string) string {
	if parts := strings.SplitN(v, ".", 3); len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

func (r *CCEManagedControlPlaneReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta2.CCEManagedControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	region, _, _, _, err := r.clusterNetwork(ctx, cluster, cp)
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

	if cp.Status.ClusterID != "" {
		info, err := svc.ShowCluster(ctx, cp.Status.ClusterID)
		if err != nil {
			// Only a 404 means the cluster is already gone. Any transient error
			// (throttle/network) must NOT fall through to removing the
			// finalizer — that would leak the CCE cluster forever.
			if !clouderrors.IsNotFound(err) {
				return resultAfterErrorForDelete(client.ObjectKeyFromObject(cp), errors.Wrap(err, "failed to check CCE cluster before deletion"))
			}
		} else {
			// Request deletion once per deletion attempt: the poll annotation
			// only suppresses re-issuing while the cluster is genuinely in the
			// Deleting phase. If the async delete failed, or the cluster fell back
			// to a non-deleting phase, an annotation-only gate would suppress the
			// retry forever — stranding the finalizer and keeping the CCE cluster
			// (and its nodes) billed with no self-healing path.
			requested := info.Phase == "Deleting" && cp.Annotations[controlPlaneDeletePollAnnotation] != ""
			if !requested {
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
					return resultAfterErrorForDelete(client.ObjectKeyFromObject(cp), errors.Wrap(err, "failed to delete CCE cluster"))
				}
				log.Info("CCE cluster deletion requested, waiting", "clusterID", cp.Status.ClusterID)
				recordEvent(r.Recorder, cp, corev1.EventTypeNormal, "ClusterDeletionRequested", "deletion requested for CCE cluster %s", cp.Status.ClusterID)
			}
			// Keep-alive: bump the poll annotation (minute granularity) so the
			// informer watch re-drives this reconcile even if the workqueue
			// dedup coalesces the delayed requeue away while the object is
			// terminating (observed live on the node-pool delete path).
			if err := r.bumpDeletePollAnnotation(ctx, cp); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
	}

	// Release the public API-server EIP this provider bound (no-op when
	// endpointAccess.public was never enabled or the EIP was already released).
	// Runs after the CCE cluster is confirmed gone so the unbind cannot fail on
	// a live master; NotFound is tolerated either way.
	if cp.Status.ControlPlaneEIPID != "" {
		if err := svc.UnbindClusterEip(ctx, cp.Status.ClusterID, cp.Status.ControlPlaneEIPID); err != nil {
			return resultAfterErrorForDelete(client.ObjectKeyFromObject(cp), err)
		}
		cp.Status.ControlPlaneEIPID = ""
	}

	// Delete the kubeconfig Secrets (CAPI + user). Both are owned by the
	// control plane, so ownership would also GC them - this explicit delete
	// keeps the behavior symmetric with the create path.
	for _, name := range []string{cp.Status.KubeconfigSecretName, cp.Spec.ClusterName + "-user-kubeconfig"} {
		if name == "" {
			continue
		}
		secret := &corev1.Secret{}
		key := types.NamespacedName{Namespace: cp.Namespace, Name: name}
		if err := r.Get(ctx, key, secret); err == nil {
			// Delete only a Secret this control plane actually owns (the create
			// path sets the controller reference). A same-named Secret created by
			// another owner must not be removed.
			if !metav1.IsControlledBy(secret, cp) && secret.Labels[clusterv1.ClusterNameLabel] != cluster.Name {
				continue
			}
			if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	controllerutil.RemoveFinalizer(cp, ControlPlaneFinalizer)
	// The delete path runs before the scope is built (see Reconcile), so it has
	// no scope.Close to persist changes — patch the finalizer removal explicitly,
	// otherwise the object stays stuck terminating forever.
	if err := r.Client.Update(ctx, cp); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// bumpDeletePollAnnotation stamps the minute-granularity keep-alive annotation
// on the control plane while a CCE cluster deletion is in flight. Its presence
// also records that DeleteCluster was already requested, so the delete call is
// not re-issued on every poll.
func (r *CCEManagedControlPlaneReconciler) bumpDeletePollAnnotation(ctx context.Context, cp *controlplanev1beta2.CCEManagedControlPlane) error {
	if cp.Annotations == nil {
		cp.Annotations = map[string]string{}
	}
	orig := cp.DeepCopy()
	cp.Annotations[controlPlaneDeletePollAnnotation] = strconv.FormatInt(time.Now().Unix()/60, 10)
	if err := r.Patch(ctx, cp, client.MergeFrom(orig)); err != nil {
		return errors.Wrap(err, "failed to bump control-plane delete poll annotation")
	}
	return nil
}

// SetupWithManager registers the controller with the manager.
func (r *CCEManagedControlPlaneReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1beta2.CCEManagedControlPlane{}).
		// Watch the per-cluster credentials Secret (<clusterName>-credentials) so
		// rotating AK/SK (or the identity Secret) takes effect on the next
		// reconcile without restarting the provider.
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.credentialsSecretToControlPlane),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
				return strings.HasSuffix(o.GetName(), credentialsSecretSuffix)
			})),
		).
		WithOptions(opts).
		Named("ccemanagedcontrolplane").
		Complete(r)
}

// credentialsSecretToControlPlane maps a credentials Secret event to the
// CCEManagedControlPlane(s) whose clusterName matches the Secret name
// (<clusterName>-credentials), so a Secret update re-reconciles them.
func (r *CCEManagedControlPlaneReconciler) credentialsSecretToControlPlane(ctx context.Context, o client.Object) []reconcile.Request {
	secret, ok := o.(*corev1.Secret)
	if !ok || !strings.HasSuffix(secret.Name, credentialsSecretSuffix) {
		return nil
	}
	clusterName := strings.TrimSuffix(secret.Name, credentialsSecretSuffix)
	cps := &controlplanev1beta2.CCEManagedControlPlaneList{}
	if err := r.Client.List(ctx, cps, client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range cps.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: cps.Items[i].Namespace, Name: cps.Items[i].Name},
		})
	}
	return reqs
}

// ---- helpers ----

// ensureKubeconfigSecret creates or rotates a kubeconfig Secret for the
// control plane. It fetches a fresh certificate-backed kubeconfig when the
// stored one is missing or its client certificate expires within the refresh
// threshold, and refuses to overwrite a pre-existing Secret the provider
// does not own (mirrors the ownership guard on the CAPI kubeconfig).
func (r *CCEManagedControlPlaneReconciler) ensureKubeconfigSecret(ctx context.Context, cp *controlplanev1beta2.CCEManagedControlPlane, cluster *clusterv1.Cluster, svc cceService.Service, clusterID, secretName string, validityDays int32) error {
	// Refresh when the certificate is near expiry OR the control-plane endpoint
	// changed (e.g. a public EIP was bound after creation): the stored secret
	// would otherwise keep pointing at the old server.
	desiredServer := ""
	if cp.Status.ControlPlaneEndpoint != nil && !cp.Status.ControlPlaneEndpoint.IsZero() {
		desiredServer = "https://" + cp.Status.ControlPlaneEndpoint.Host + ":" + strconv.Itoa(int(cp.Status.ControlPlaneEndpoint.Port))
	}
	if !kubeconfigNeedsRefresh(ctx, r.Client, cp.Namespace, secretName, kubeconfigRefreshThresholdDays) &&
		kubeconfigSecretHasServer(ctx, r.Client, cp.Namespace, secretName, desiredServer) {
		return nil
	}
	kubeconfig, err := svc.GetClusterKubeconfig(ctx, clusterID, validityDays)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cp.Namespace,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: cluster.Name},
		},
		Data: map[string][]byte{"value": []byte(kubeconfig)},
	}
	// Own the Secret so lifecycle is tied to the control plane.
	if err := controllerutil.SetControllerReference(cp, secret, r.Client.Scheme()); err != nil {
		return err
	}
	if err := r.Client.Create(ctx, secret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: cp.Namespace, Name: secretName}, existing); err != nil {
			return err
		}
		if !metav1.IsControlledBy(existing, cp) && existing.Labels[clusterv1.ClusterNameLabel] != cluster.Name {
			return errors.Errorf("Secret %s exists and is not owned by this provider; refusing to overwrite", secretName)
		}
		existing.Data = secret.Data
		if err := r.Update(ctx, existing); err != nil {
			return err
		}
	}
	return nil
}

// clusterNetwork reads the region and host network (VPC + node subnet +
// ENI container subnets) from the CCECluster shell (infrastructureRef) -
// official hostNetwork is required at cluster creation (A2, verified by
// the real CCE smoke test). Managed subnets report their ResourceID; the
// ENI subnets carry the neutron_subnet_id the eniNetwork API requires.
func (r *CCEManagedControlPlaneReconciler) clusterNetwork(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta2.CCEManagedControlPlane) (region, vpcID, subnetID string, eniSubnets []string, err error) {
	if cluster.Spec.InfrastructureRef.Name == "" {
		return "", "", "", nil, errors.New("cluster has no infrastructureRef")
	}
	cceCluster := &infrav1beta2.CCECluster{}
	key := types.NamespacedName{Namespace: cp.Namespace, Name: cluster.Spec.InfrastructureRef.Name}
	if err := r.Get(ctx, key, cceCluster); err != nil {
		return "", "", "", nil, errors.Wrapf(err, "failed to get CCECluster %s", key)
	}
	if cceCluster.Spec.Region == "" {
		return "", "", "", nil, errors.New("CCECluster spec.region is empty")
	}
	vpcID = cceCluster.Spec.Network.VPC.ID
	if vpcID == "" {
		vpcID = cceCluster.Spec.Network.VPC.ResourceID
	}
	for _, s := range cceCluster.Spec.Network.Subnets {
		id := s.ID
		if id == "" {
			id = s.ResourceID
		}
		if s.Type == common.SubnetTypeENI {
			// The eniNetwork API consumes the neutron_subnet_id.
			neutron := s.NeutronSubnetID
			if neutron == "" {
				neutron = id
			}
			if neutron != "" {
				eniSubnets = append(eniSubnets, neutron)
			}
			continue
		}
		if subnetID == "" && id != "" {
			subnetID = id
		}
	}
	return cceCluster.Spec.Region, vpcID, subnetID, eniSubnets, nil
}

// toCreateClusterInput maps the control plane spec to the CreateCluster
// input. identityAgency (from a CCEClusterRoleIdentity, when one is
// referenced) fills the cluster agency when the spec does not set it
// explicitly - an explicit spec.agencyName always wins.
func toCreateClusterInput(cp *controlplanev1beta2.CCEManagedControlPlane, vpcID, nodeSubnetID, identityAgency string, eniSubnets []string) cceService.CreateClusterInput {
	agency := cp.Spec.AgencyName
	if agency == "" {
		agency = identityAgency
	}
	return cceService.CreateClusterInput{
		Name:                  cp.Spec.ClusterName,
		Category:              cp.Spec.Category,
		Flavor:                cp.Spec.Flavor,
		Version:               cp.Spec.Version,
		ContainerNetworkMode:  cp.Spec.ContainerNetwork.Mode,
		ContainerNetworkCIDR:  cp.Spec.ContainerNetwork.CIDR,
		ContainerNetworkCIDRs: cp.Spec.ContainerNetwork.CIDRs,
		ENISubnets:            eniSubnets,
		HostNetworkVpcID:      vpcID,
		HostNetworkSubnetID:   nodeSubnetID,
		ServiceCIDR:           cp.Spec.ServiceNetwork.CIDR,
		ServiceIPv6CIDR:       cp.Spec.ServiceNetwork.IPv6CIDR,
		Ipv6Enable:            cp.Spec.Ipv6Enable,
		EnableAutopilot:       cp.Spec.EnableAutopilot,
		CustomSAN:             cp.Spec.CustomSan,
		PublicAccess:          cp.Spec.EndpointAccess.Public,
		PublicAccessCIDRs:     cp.Spec.EndpointAccess.CIDRs,
		AgencyName:            agency,
		BillingMode:           cp.Spec.Billing.Mode,
		EncryptionConfig:      toEncryptionConfigInput(cp.Spec.EncryptionConfig),
		Authentication:        toAuthenticationInput(cp.Spec.Authentication),
		Tags:                  map[string]string(cp.Spec.AdditionalTags),
		EnableDataPlaneV2:     cp.Spec.EnableDataPlaneV2 != nil && *cp.Spec.EnableDataPlaneV2,
	}
}

// toEncryptionConfigInput maps the spec to the service-layer input; nil
// when the spec is unset (the CCE API then applies its Default).
func toEncryptionConfigInput(spec *controlplanev1beta2.EncryptionConfigSpec) *cceService.EncryptionConfigInput {
	if spec == nil {
		return nil
	}
	return &cceService.EncryptionConfigInput{Mode: spec.Mode}
}

// toAuthenticationInput maps the spec to the service-layer input; nil when
// the spec is unset (the CCE API then applies rbac).
func toAuthenticationInput(spec *controlplanev1beta2.AuthenticationSpec) *cceService.AuthenticationInput {
	if spec == nil {
		return nil
	}
	in := &cceService.AuthenticationInput{Mode: spec.Mode}
	if spec.AuthenticatingProxy != nil {
		in.AuthenticatingProxy = &cceService.AuthenticatingProxyInput{
			CA:         spec.AuthenticatingProxy.CA,
			Cert:       spec.AuthenticatingProxy.Cert,
			PrivateKey: spec.AuthenticatingProxy.PrivateKey,
		}
	}
	return in
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
// missing, upgrade version drift, delete those this provider previously applied
// and that are no longer listed. Deletion is gated on status.addons so platform
// default addons (and any addon this provider never declared) are never removed.
func (r *CCEManagedControlPlaneReconciler) reconcileAddons(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta2.CCEManagedControlPlane) error {
	log := ctrl.LoggerFrom(ctx)
	if len(cp.Spec.Addons) == 0 && !capiconditions.Has(cp, conditions.AddonsConfiguredCondition) {
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
	specByName := map[string]controlplanev1beta2.AddonSpec{}
	for _, a := range cp.Spec.Addons {
		specByName[a.Name] = a
	}
	managed := map[string]bool{}
	for _, name := range cp.Status.Addons {
		managed[name] = true
	}

	// Create missing / upgrade drift.
	created := map[string]bool{}
	for _, want := range cp.Spec.Addons {
		got, exists := cloudByName[want.Name]
		switch {
		case !exists:
			if _, err := svc.CreateAddonInstance(ctx, cceService.AddonInput{
				ClusterID: clusterID, Name: want.Name, Version: want.Version,
			}); err != nil {
				return err
			}
			created[want.Name] = true
		case want.Version != "" && want.Version != got.Version:
			// Upgrading drift is declarative, but does not transfer ownership:
			// an addon that already existed (a platform default, or one managed
			// elsewhere) stays unmanaged, so removing it from the spec later
			// does not delete a resource this provider did not create. CCE
			// addons carry no resource tags, so status.addons is the only
			// ownership ledger available.
			if err := svc.UpdateAddonInstance(ctx, cceService.AddonInput{
				ClusterID: clusterID, AddonID: got.ID, Name: want.Name, Version: want.Version,
			}); err != nil {
				return err
			}
		}
	}
	// Remove addons this provider applied that are no longer listed. Addons
	// never applied here (platform defaults, addons managed elsewhere) are
	// skipped so they survive a declarative set change.
	for _, got := range current {
		if _, keep := specByName[got.Name]; keep {
			continue
		}
		if !managed[got.Name] {
			log.V(4).Info("Skipping addon deletion: not managed by this provider", "addon", got.Name)
			continue
		}
		if err := svc.DeleteAddonInstance(ctx, clusterID, got.ID); err != nil {
			return err
		}
	}
	// The ownership ledger: only addons this provider created (or already
	// owned) are recorded. A foreign addon that was merely upgraded above is
	// not adopted, so a later spec change cannot delete it.
	var applied []string
	for _, want := range cp.Spec.Addons {
		if !managed[want.Name] && !created[want.Name] {
			continue
		}
		applied = append(applied, want.Name)
	}
	cp.Status.Addons = applied
	return nil
}

// reconcilePodIdentityAssociations reconciles the declared pod-identity
// associations against the cloud. Ownership follows the provider owned tag
// (the upstream reference keys EKS pod-identity ownership on a resource tag):
// only associations carrying the owned tag are managed — created when
// declared, deleted when removed. An association created out of band (no owned
// tag) is never adopted (CCE cannot add tags after create) and never deleted.
func (r *CCEManagedControlPlaneReconciler) reconcilePodIdentityAssociations(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta2.CCEManagedControlPlane) error {
	log := ctrl.LoggerFrom(ctx)
	if len(cp.Spec.PodIdentityAssociations) == 0 && !capiconditions.Has(cp, conditions.PodIdentityAssociationsConfiguredCondition) {
		return nil
	}
	current, err := svc.ListPodIdentityAssociations(ctx, clusterID)
	if err != nil {
		return err
	}
	key := func(ns, sa string) string { return ns + "/" + sa }
	ownedTag := tags.OwnedTagKey(cp.Spec.ClusterName)
	presentByKey := map[string]cceService.PodIdentityAssociationInfo{}
	managedByKey := map[string]cceService.PodIdentityAssociationInfo{}
	for _, a := range current {
		k := key(a.Namespace, a.ServiceAccount)
		presentByKey[k] = a
		if a.Tags[ownedTag] == "owned" {
			managedByKey[k] = a
		}
	}
	specByKey := map[string]bool{}
	for _, a := range cp.Spec.PodIdentityAssociations {
		specByKey[key(a.Namespace, a.ServiceAccount)] = true
	}

	// Create missing, stamping the ownership tag.
	for _, want := range cp.Spec.PodIdentityAssociations {
		k := key(want.Namespace, want.ServiceAccount)
		if _, ours := managedByKey[k]; ours {
			continue
		}
		if _, foreign := presentByKey[k]; foreign {
			// The desired association already exists but is not ours; leave it
			// alone rather than erroring or adopting a resource we cannot tag.
			log.V(4).Info("Skipping pod-identity association: an association for this service account exists but is not owned by this provider",
				"namespace", want.Namespace, "serviceAccount", want.ServiceAccount)
			continue
		}
		if _, err := svc.CreatePodIdentityAssociation(ctx, cceService.PodIdentityAssociationInput{
			ClusterID:      clusterID,
			Namespace:      want.Namespace,
			ServiceAccount: want.ServiceAccount,
			AgencyName:     want.AgencyName,
			Tags:           map[string]string{ownedTag: "owned"},
		}); err != nil {
			return err
		}
	}
	// Delete removed associations this provider owns.
	for k, got := range managedByKey {
		if specByKey[k] {
			continue
		}
		if err := svc.DeletePodIdentityAssociation(ctx, clusterID, got.ID); err != nil {
			return err
		}
	}
	return nil
}

// reconcileLogging reconciles the declared control-plane log collection config
// against the cloud. Declarative: TTL + the exact
// log item set, compared against ShowClusterConfig, applied via
// UpdateClusterLogConfig on drift.
func (r *CCEManagedControlPlaneReconciler) reconcileLogging(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta2.CCEManagedControlPlane) error {
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
// the account: create missing, update drift (policyType/principal/namespaces),
// delete those this provider previously applied and that are no longer listed.
// CCE access policies are account-scoped (one policy may span many clusters),
// so management is gated on BOTH the policy scope (it must include this
// cluster's ID, or the "*" wildcard) AND status.accessPolicies (only policies
// this provider created). A same-named policy scoped to other clusters only is
// never adopted, updated or deleted.
func (r *CCEManagedControlPlaneReconciler) reconcileAccessPolicies(ctx context.Context, svc cceService.Service, clusterID string, cp *controlplanev1beta2.CCEManagedControlPlane) error {
	log := ctrl.LoggerFrom(ctx)
	if len(cp.Spec.AccessPolicies) == 0 && !capiconditions.Has(cp, conditions.AccessPoliciesConfiguredCondition) {
		return nil
	}
	current, err := svc.ListAccessPolicies(ctx)
	if err != nil {
		return err
	}
	// Candidate map scoped to this cluster: an out-of-scope policy (same name,
	// other clusters only) is never a candidate, so it can neither satisfy our
	// spec nor be updated as if it were ours.
	cloudByName := map[string]cceService.AccessPolicyInfo{}
	for _, p := range current {
		if !accessPolicyInScope(p, clusterID) {
			continue
		}
		cloudByName[p.Name] = p
	}
	specByName := map[string]controlplanev1beta2.AccessPolicySpec{}
	for _, p := range cp.Spec.AccessPolicies {
		specByName[p.Name] = p
	}
	managed := map[string]bool{}
	for _, name := range cp.Status.AccessPolicies {
		managed[name] = true
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
		case !managed[want.Name]:
			// The name resolves to an in-scope policy this provider did not
			// create. CCE access policies carry no ownership tag, so it cannot be
			// adopted; skipping it silently while marking the condition True would
			// claim a policy is configured that never will be (the upstream
			// reference fails the reconcile on this collision).
			return errors.Errorf("access policy %q already exists in the scope of cluster %s but is not managed by this provider; remove the conflicting policy or rename the declared one", want.Name, clusterID)
		case accessPolicyDrifted(got, want):
			if err := svc.UpdateAccessPolicy(ctx, got.PolicyID, input); err != nil {
				return err
			}
		}
	}
	// Remove policies this provider applied that are no longer listed; never
	// touch policies owned by another cluster (the list is account-scoped).
	for _, got := range current {
		if _, keep := specByName[got.Name]; keep {
			continue
		}
		if !managed[got.Name] || !accessPolicyInScope(got, clusterID) {
			log.V(4).Info("Skipping access policy deletion: not managed by this provider", "policy", got.Name)
			continue
		}
		if err := svc.DeleteAccessPolicy(ctx, got.PolicyID); err != nil {
			return err
		}
	}
	// Every declared policy is now either created, owned, or the reconcile
	// errored above; latch the full declared set as the ownership ledger.
	var applied []string
	for _, want := range cp.Spec.AccessPolicies {
		applied = append(applied, want.Name)
	}
	cp.Status.AccessPolicies = applied
	return nil
}

// accessPolicyInScope reports whether a cloud access policy applies to
// clusterID: an explicit cluster ID match, or the "*" wildcard (all
// clusters). A policy scoped only to other clusters (or with no scope
// reported) is not ours to manage (B4).
func accessPolicyInScope(p cceService.AccessPolicyInfo, clusterID string) bool {
	for _, id := range p.ClusterIDs {
		if id == clusterID || id == "*" {
			return true
		}
	}
	return false
}

// toAccessPolicyInput maps a spec to the service input, scoping the policy to
// the owning cluster.
func toAccessPolicyInput(clusterID string, p controlplanev1beta2.AccessPolicySpec) cceService.AccessPolicyInput {
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
func accessPolicyDrifted(got cceService.AccessPolicyInfo, want controlplanev1beta2.AccessPolicySpec) bool {
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
