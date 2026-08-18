/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"

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
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	if controllerutil.AddFinalizer(cp, ControlPlaneFinalizer) {
		if err := r.Update(ctx, cp); err != nil {
			return ctrl.Result{}, err
		}
	}

	region, err := r.clusterRegion(ctx, cluster, cp)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	// Credentials: per-cluster Secret (<cluster>-credentials) with env fallback.
	creds, err := scope.ResolveCredentials(ctx, r.Client, cp.Namespace, cp.Spec.ClusterName+"-credentials")
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(cp, conditions.CredentialsReadyCondition, "CredentialsResolved", "CCE credentials resolved")

	svc, err := cceService.NewClient(region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		conditions.MarkFalse(cp, conditions.CredentialsReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	// Ensure the CCE cluster exists (idempotent create).
	clusterID := cp.Status.ClusterID
	if clusterID == "" {
		id, err := svc.CreateCluster(ctx, toCreateClusterInput(cp))
		if err != nil {
			conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		clusterID = id
		cp.Status.ClusterID = id
	}

	// Wait for the cluster to become Available, then backfill the endpoint.
	info, err := svc.ShowCluster(ctx, clusterID)
	if err != nil {
		if clouderrors.IsNotFound(err) {
			// Cluster deleted out of band — reset so it is recreated.
			cp.Status.ClusterID = ""
			conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
				conditions.ReconciliationFailedReason, "CCE cluster not found, recreating")
			return ctrl.Result{RequeueAfter: defaultRequeue}, nil
		}
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, err
	}
	if info.Phase != "Available" {
		conditions.MarkFalse(cp, conditions.CCEClusterReadyCondition,
			conditions.ReconciliationInProgressReason, "CCE cluster phase: "+info.Phase)
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	// Backfill the API server endpoint (official ShowClusterEndpoints model).
	for _, ep := range info.Endpoints {
		if ep.Type == "public" || (ep.Type == "private" && cp.Status.ControlPlaneEndpoint.IsZero()) {
			cp.Status.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: ep.URL, Port: 5443}
			cp.Spec.ControlPlaneEndpoint = cp.Status.ControlPlaneEndpoint
		}
	}
	cp.Status.Version = info.Version
	conditions.MarkTrue(cp, conditions.CCEClusterReadyCondition, "ClusterAvailable", "CCE cluster is available")

	// kubeconfig Secret (mirrors the ACK provider kubeconfig contract, so
	// `clusterctl get kubeconfig` works).
	if cp.Status.KubeconfigSecretName == "" {
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
		if err := r.Client.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
			conditions.MarkFalse(cp, conditions.KubeconfigReadyCondition,
				conditions.ReconciliationFailedReason, err.Error())
			return ctrl.Result{}, err
		}
		cp.Status.KubeconfigSecretName = secretName
	}
	conditions.MarkTrue(cp, conditions.KubeconfigReadyCondition, "KubeconfigGenerated", "kubeconfig Secret generated")

	cp.Status.Ready = true
	cp.Status.Initialized = true
	log.Info("CCE control plane is ready", "clusterID", clusterID)
	return ctrl.Result{}, nil
}

func (r *CCEManagedControlPlaneReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	region, err := r.clusterRegion(ctx, cluster, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	creds, err := scope.ResolveCredentials(ctx, r.Client, cp.Namespace, cp.Spec.ClusterName+"-credentials")
	if err != nil {
		return ctrl.Result{}, err
	}
	svc, err := cceService.NewClient(region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cp.Status.ClusterID != "" {
		if _, err := svc.ShowCluster(ctx, cp.Status.ClusterID); err == nil {
			// TODO(P0): deletion semantics (cascade node pools, duration,
			// leftovers) — questionnaire Q8.
			if err := svc.DeleteCluster(ctx, cp.Status.ClusterID); err != nil && !clouderrors.IsNotFound(err) {
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

// clusterRegion reads the region from the CCECluster shell (infrastructureRef).
func (r *CCEManagedControlPlaneReconciler) clusterRegion(ctx context.Context, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane) (string, error) {
	if cluster.Spec.InfrastructureRef.Name == "" {
		return "", errors.New("cluster has no infrastructureRef")
	}
	cceCluster := &infrav1beta1.CCECluster{}
	key := types.NamespacedName{Namespace: cp.Namespace, Name: cluster.Spec.InfrastructureRef.Name}
	if err := r.Get(ctx, key, cceCluster); err != nil {
		return "", errors.Wrapf(err, "failed to get CCECluster %s", key)
	}
	if cceCluster.Spec.Region == "" {
		return "", errors.New("CCECluster spec.region is empty")
	}
	return cceCluster.Spec.Region, nil
}

func toCreateClusterInput(cp *controlplanev1beta1.CCEManagedControlPlane) cceService.CreateClusterInput {
	return cceService.CreateClusterInput{
		Name:                 cp.Spec.ClusterName,
		Category:             cp.Spec.Category,
		Flavor:               cp.Spec.Flavor,
		Version:              cp.Spec.Version,
		ContainerNetworkMode: cp.Spec.ContainerNetwork.Mode,
		ContainerNetworkCIDR: cp.Spec.ContainerNetwork.CIDR,
		ENISubnets:           cp.Spec.ContainerNetwork.ENISubnets,
		ServiceCIDR:          cp.Spec.ServiceNetwork.CIDR,
		CustomSAN:            cp.Spec.CustomSan,
		PublicAccess:         cp.Spec.EndpointAccess.Public,
		AgencyName:           cp.Spec.AgencyName,
		BillingMode:          cp.Spec.Billing.Mode,
	}
}
