/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	sdkerr "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	iamService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/iam"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

// ctxBG is a shared background context for controller tests.
var ctxBG = context.Background()

func TestControlPlaneReconcileWaitingForInfra(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-waiting"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	// Do NOT mark infrastructure provisioned.

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter != defaultRequeue {
		t.Errorf("expected requeue %v, got %v", defaultRequeue, res.RequeueAfter)
	}
	if len(fakeSvc.CreatedClusters) != 0 {
		t.Error("expected no cluster creation while infrastructure not provisioned")
	}
	_ = cluster
}

func TestControlPlaneReconcileSuccess(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-success"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if !got.Status.Ready || !got.Status.Initialized {
		t.Error("expected control plane Ready and Initialized")
	}
	if got.Status.ClusterID != "cluster-1" {
		t.Errorf("expected ClusterID cluster-1, got %q", got.Status.ClusterID)
	}
	if got.Status.ControlPlaneEndpoint == nil || got.Status.ControlPlaneEndpoint.Host != "10.0.0.10" || got.Status.ControlPlaneEndpoint.Port != 5443 {
		t.Errorf("unexpected endpoint: %+v", got.Status.ControlPlaneEndpoint)
	}
	for _, cType := range []string{conditions.CredentialsReadyCondition, conditions.CCEClusterReadyCondition, conditions.KubeconfigReadyCondition} {
		if c := capiconditions.Get(got, cType); c == nil || c.Status != metav1.ConditionTrue {
			t.Errorf("expected condition %s=True, got %v", cType, c)
		}
	}

	// Cluster creation input carried the CRD values (absolute scaling target
	// etc.) and the kubeconfig Secret was created.
	if len(fakeSvc.CreatedClusters) != 1 {
		t.Fatalf("expected 1 created cluster, got %d", len(fakeSvc.CreatedClusters))
	}
	in := fakeSvc.CreatedClusters[0]
	if in.Name != "test-cluster" || in.Category != "Turbo" || in.ContainerNetworkMode != "eni" {
		t.Errorf("unexpected create input: %+v", in)
	}
	if fakeSvc.KubeconfigCalls != 2 {
		t.Errorf("expected 2 kubeconfig calls (CAPI + user), got %d", fakeSvc.KubeconfigCalls)
	}
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "test-cluster-kubeconfig"}, secret); err != nil {
		t.Fatalf("expected kubeconfig Secret: %v", err)
	}
	if len(secret.Data["value"]) == 0 {
		t.Error("expected kubeconfig Secret data")
	}
	// The user kubeconfig is an
	// independent credential owned by the control plane.
	userSecret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "test-cluster-user-kubeconfig"}, userSecret); err != nil {
		t.Fatalf("expected user kubeconfig Secret: %v", err)
	}
	if len(userSecret.Data["value"]) == 0 {
		t.Error("expected user kubeconfig Secret data")
	}
}

func TestControlPlaneReconcileDeletePassesOptions(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-delete"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	// Make it ready first so Status.ClusterID is set and deletion has an ID.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile failed: %v", err)
	}

	// Trigger deletion via Delete() (deletionTimestamp is set by the API
	// server; the object survives because the controller added a finalizer).
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to re-get control plane: %v", err)
	}
	if err := k8sClient.Delete(ctx, latest); err != nil {
		t.Fatalf("failed to delete control plane: %v", err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("delete reconcile returned error: %v", err)
	}

	if len(fakeSvc.DeletedClusters) != 1 {
		t.Fatalf("expected 1 DeleteCluster call, got %d", len(fakeSvc.DeletedClusters))
	}
	d := fakeSvc.DeletedClusters[0]
	if d.ClusterID != "cluster-1" || !d.DeleteEVS || !d.DeleteENI || !d.DeleteELB || d.OnDemandNodePolicy != "delete" {
		t.Errorf("unexpected delete input (delete options must avoid EVS leftovers): %+v", d)
	}

	// Finalizer removed and kubeconfig Secret gone (delete is async, so the
	// controller keeps requeueing until the cluster disappears; here the fake
	// returns success immediately, so the finalizer is removed on the next
	// reconcile — the first delete reconcile already requested deletion).
}

// upgradeCP builds the shared CP upgrade scenario: spec.version v1.31.0 while
// the cloud reports v1.30.0.
func upgradeCP(t *testing.T, ns string) (*clusterv1.Cluster, *controlplanev1beta2.CCEManagedControlPlane, *fakes.FakeCCEService, *CCEManagedControlPlaneReconciler) {
	t.Helper()
	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Spec.Version = "v1.31.0"
	if err := k8sClient.Update(ctxBG, cp); err != nil {
		t.Fatalf("failed to set spec.version: %v", err)
	}
	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	return cluster, cp, fakeSvc, r
}

func TestControlPlaneReconcileUpgradeStart(t *testing.T) {
	ns := "cp-test-upg-start"
	createNamespace(t, ns)
	_, cp, fakeSvc, r := upgradeCP(t, ns)

	res, err := r.Reconcile(ctxBG, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter != defaultRequeue {
		t.Errorf("expected requeue %v during upgrade, got %v", defaultRequeue, res.RequeueAfter)
	}
	if len(fakeSvc.StartUpgradeCalls) != 1 || fakeSvc.StartUpgradeCalls[0] != "v1.31.0" {
		t.Fatalf("expected StartUpgrade(v1.31.0), got %v", fakeSvc.StartUpgradeCalls)
	}
	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctxBG, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if got.Status.UpgradeTaskID != "upgrade-task-1" {
		t.Errorf("expected UpgradeTaskID upgrade-task-1, got %q", got.Status.UpgradeTaskID)
	}
	if c := capiconditions.Get(got, conditions.UpgradeReadyCondition); c == nil || c.Status != metav1.ConditionFalse {
		t.Errorf("expected UpgradeReady=False while upgrading, got %v", c)
	}
	// The control plane must not report Ready while the upgrade is in flight.
	if got.Status.Ready {
		t.Error("expected Ready=false during upgrade")
	}
}

func TestControlPlaneReconcileUpgradeNotOffered(t *testing.T) {
	ns := "cp-test-upg-notoffered"
	createNamespace(t, ns)
	_, cp, fakeSvc, r := upgradeCP(t, ns)
	fakeSvc.GetUpgradeInfoFn = func(_ context.Context, _ string) (*cceService.UpgradeInfo, error) {
		// Platform offers no targets (questionnaire Q11, verified live).
		return &cceService.UpgradeInfo{CurrentVersion: "v1.30.0", TargetVersions: []string{}}, nil
	}

	_, err := r.Reconcile(ctxBG, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.StartUpgradeCalls) != 0 {
		t.Errorf("expected no upgrade start when no targets offered, got %v", fakeSvc.StartUpgradeCalls)
	}
	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctxBG, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	c := capiconditions.Get(got, conditions.UpgradeReadyCondition)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != conditions.UpgradeNotOfferedReason {
		t.Errorf("expected UpgradeReady=False/UpgradeNotOffered, got %v", c)
	}
	if got.Status.Ready {
		t.Error("expected Ready=false when upgrade cannot proceed")
	}
}

func TestControlPlaneReconcileUpgradeCompletes(t *testing.T) {
	ns := "cp-test-upg-complete"
	createNamespace(t, ns)
	_, cp, fakeSvc, r := upgradeCP(t, ns)

	// First reconcile starts the upgrade task.
	if _, err := r.Reconcile(ctxBG, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}
	if len(fakeSvc.StartUpgradeCalls) != 1 {
		t.Fatalf("expected upgrade start, got %v", fakeSvc.StartUpgradeCalls)
	}

	// Cloud now reports the new version (upgrade done); the fake task
	// already returns Success by default.
	fakeSvc.ShowClusterFn = func(_ context.Context, clusterID string) (*cceService.ClusterInfo, error) {
		return &cceService.ClusterInfo{ClusterID: clusterID, Phase: "Available", Version: "v1.31.0"}, nil
	}

	if _, err := r.Reconcile(ctxBG, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}
	if len(fakeSvc.StartUpgradeCalls) != 1 {
		t.Errorf("expected no new upgrade after completion, got %v", fakeSvc.StartUpgradeCalls)
	}
	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctxBG, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if got.Status.UpgradeTaskID != "" {
		t.Errorf("expected UpgradeTaskID cleared after success, got %q", got.Status.UpgradeTaskID)
	}
	if got.Status.Version != "v1.31.0" {
		t.Errorf("expected status.version v1.31.0, got %q", got.Status.Version)
	}
	if c := capiconditions.Get(got, conditions.UpgradeReadyCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected UpgradeReady=True after success, got %v", c)
	}

	// Success now persists + requeues (so the next reconcile observes the new
	// version and does not re-trigger an upgrade in the same pass). The next
	// reconcile should then complete kubeconfig + Ready.
	if _, err := r.Reconcile(ctxBG, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("third Reconcile returned error: %v", err)
	}
	if len(fakeSvc.StartUpgradeCalls) != 1 {
		t.Errorf("expected no new upgrade after completion, got %v", fakeSvc.StartUpgradeCalls)
	}
	if err := k8sClient.Get(ctxBG, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if !got.Status.Ready || !got.Status.Initialized {
		t.Error("expected control plane Ready after upgrade completed")
	}
}

// TestControlPlaneReconcileAddons verifies declarative addon management:
// create missing, upgrade version drift, and delete only what this provider
// created — a merely-upgraded pre-existing addon is not adopted (M2).
func TestControlPlaneReconcileAddons(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-addons"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	// Declare two addons; one already on the cloud at a stale version.
	cp.Spec.Addons = []controlplanev1beta2.AddonSpec{
		{Name: "coredns", Version: "1.2.0"},
		{Name: "metrics-server", Version: ""}, // latest
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to update control plane spec: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// Cloud has coredns at 1.1.0 (drift -> upgrade) + an addon to remove.
	fakeSvc.Addons = []cceService.AddonInfo{
		{ID: "addon-id-coredns", Name: "coredns", Version: "1.1.0", Status: "running"},
		{ID: "addon-id-old", Name: "old-addon", Version: "1.0.0", Status: "running"},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// metrics-server must be created, coredns upgraded (drift). "old-addon"
	// was never declared by this provider, so it must NOT be deleted (B5).
	if len(fakeSvc.AddonCreateCalls) != 1 || fakeSvc.AddonCreateCalls[0].Name != "metrics-server" {
		t.Errorf("expected create metrics-server, got %+v", fakeSvc.AddonCreateCalls)
	}
	if len(fakeSvc.AddonUpdateCalls) != 1 || fakeSvc.AddonUpdateCalls[0].Name != "coredns" || fakeSvc.AddonUpdateCalls[0].Version != "1.2.0" {
		t.Errorf("expected upgrade coredns to 1.2.0, got %+v", fakeSvc.AddonUpdateCalls)
	}
	if len(fakeSvc.AddonDeleteCalls) != 0 {
		t.Errorf("undeclared addon must never be deleted, got %v", fakeSvc.AddonDeleteCalls)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.AddonsConfiguredCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected AddonsConfigured=True, got %v", c)
	}
	// Only the addon this provider created is latched as owned; the pre-existing
	// coredns it merely upgraded is not adopted (M2).
	if len(got.Status.Addons) != 1 || got.Status.Addons[0] != "metrics-server" {
		t.Errorf("expected status.addons [metrics-server], got %v", got.Status.Addons)
	}

	// Emptying the spec removes only the addon this provider created
	// (metrics-server). coredns was merely upgraded, not created, so it is not
	// owned and must survive (M2); old-addon was never declared at all.
	fakeSvc.Addons = []cceService.AddonInfo{
		{ID: "addon-id-coredns", Name: "coredns", Version: "1.2.0", Status: "running"},
		{ID: "addon-id-metrics-server", Name: "metrics-server", Version: "1.0.0", Status: "running"},
		{ID: "addon-id-old", Name: "old-addon", Version: "1.0.0", Status: "running"},
	}
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	latest.Spec.Addons = nil
	if err := k8sClient.Update(ctx, latest); err != nil {
		t.Fatalf("failed to empty addons: %v", err)
	}
	fakeSvc.AddonDeleteCalls = nil
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile (empty) returned error: %v", err)
	}
	deleted := map[string]bool{}
	for _, id := range fakeSvc.AddonDeleteCalls {
		deleted[id] = true
	}
	if !deleted["addon-id-metrics-server"] {
		t.Errorf("expected metrics-server deleted after emptying spec, got %v", fakeSvc.AddonDeleteCalls)
	}
	if deleted["addon-id-coredns"] {
		t.Errorf("a merely-upgraded foreign addon must not be deleted, got %v", fakeSvc.AddonDeleteCalls)
	}
	if deleted["addon-id-old"] {
		t.Errorf("undeclared old-addon must not be deleted, got %v", fakeSvc.AddonDeleteCalls)
	}
}

// TestControlPlaneReconcilePodIdentity verifies declarative pod-identity
// association management (create missing, delete removed) and the ownership
// boundary: only associations carrying the provider owned tag are managed; a
// foreign untagged association is never touched.
func TestControlPlaneReconcilePodIdentity(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-podid"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.PodIdentityAssociations = []controlplanev1beta2.PodIdentityAssociationSpec{
		{Namespace: "default", ServiceAccount: "app-sa", AgencyName: "app-agency"},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to update control plane spec: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// An owned association no longer in spec -> deleted; a foreign (untagged)
	// association -> never touched.
	fakeSvc.PodIdentities = []cceService.PodIdentityAssociationInfo{
		{ID: "podid-old", Namespace: "kube-system", ServiceAccount: "old-sa", AgencyName: "old-agency",
			Tags: map[string]string{cceService.OwnedTagKey("test-cluster"): "owned"}},
		{ID: "podid-foreign", Namespace: "kube-system", ServiceAccount: "foreign-sa", AgencyName: "foreign-agency"},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(fakeSvc.PodIdentityCreate) != 1 {
		t.Fatalf("expected 1 create, got %d", len(fakeSvc.PodIdentityCreate))
	}
	created := fakeSvc.PodIdentityCreate[0]
	if created.Namespace != "default" || created.ServiceAccount != "app-sa" || created.AgencyName != "app-agency" {
		t.Errorf("unexpected create input: %+v", created)
	}
	if created.Tags[cceService.OwnedTagKey("test-cluster")] != "owned" {
		t.Errorf("created association must carry the provider owned tag, got %v", created.Tags)
	}
	if len(fakeSvc.PodIdentityDelete) != 1 || fakeSvc.PodIdentityDelete[0] != "podid-old" {
		t.Errorf("expected only the owned association deleted, got %v", fakeSvc.PodIdentityDelete)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.PodIdentityAssociationsConfiguredCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected PodIdentityAssociationsConfigured=True, got %v", c)
	}
}

// TestControlPlaneReconcileAddonsScopedDeletion covers B5 end to end: the
// addon declared by this provider is removed once the spec is emptied, while
// platform-default addons (coredns/everest) are never deleted.
func TestControlPlaneReconcileAddonsScopedDeletion(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-addons-scoped"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.Addons = []controlplanev1beta2.AddonSpec{{Name: "metrics-server"}}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set addons: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// Platform-default addons the provider never declared.
	fakeSvc.Addons = []cceService.AddonInfo{
		{ID: "addon-id-coredns", Name: "coredns", Version: "1.0.0", Status: "running"},
		{ID: "addon-id-everest", Name: "everest", Version: "1.0.0", Status: "running"},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.AddonCreateCalls) != 1 || fakeSvc.AddonCreateCalls[0].Name != "metrics-server" {
		t.Errorf("expected create metrics-server, got %+v", fakeSvc.AddonCreateCalls)
	}
	if len(fakeSvc.AddonDeleteCalls) != 0 {
		t.Fatalf("platform-default addons must never be deleted, got %v", fakeSvc.AddonDeleteCalls)
	}

	// Empty the spec: our addon is removed, the platform defaults stay.
	fakeSvc.Addons = append(fakeSvc.Addons,
		cceService.AddonInfo{ID: "addon-id-metrics-server", Name: "metrics-server", Version: "1.0.0", Status: "running"})
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	latest.Spec.Addons = nil
	if err := k8sClient.Update(ctx, latest); err != nil {
		t.Fatalf("failed to empty addons: %v", err)
	}
	fakeSvc.AddonDeleteCalls = nil
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile (empty) returned error: %v", err)
	}
	deleted := map[string]bool{}
	for _, id := range fakeSvc.AddonDeleteCalls {
		deleted[id] = true
	}
	if !deleted["addon-id-metrics-server"] {
		t.Errorf("expected metrics-server deleted after emptying spec, got %v", fakeSvc.AddonDeleteCalls)
	}
	if deleted["addon-id-coredns"] || deleted["addon-id-everest"] {
		t.Errorf("platform-default addons must never be deleted, got %v", fakeSvc.AddonDeleteCalls)
	}
}

// TestControlPlaneReconcileAddonsNeverDeclaredNoDelete covers B5's safety
// latch: a cluster that never declared addons must not delete anything on its
// first reconcile.
func TestControlPlaneReconcileAddonsNeverDeclaredNoDelete(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-addons-undeclared"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.Addons = []cceService.AddonInfo{
		{ID: "addon-id-coredns", Name: "coredns", Version: "1.0.0", Status: "running"},
		{ID: "addon-id-everest", Name: "everest", Version: "1.0.0", Status: "running"},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.AddonDeleteCalls) != 0 {
		t.Fatalf("a cluster that never declared addons must delete nothing, got %v", fakeSvc.AddonDeleteCalls)
	}
}

// TestControlPlaneReconcilePodIdentityEmptySpecCleanup covers B3: once pod
// identity associations have been configured, emptying the spec must run the
// delete-difference loop instead of short-circuiting.
func TestControlPlaneReconcilePodIdentityEmptySpecCleanup(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-podid-cleanup"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.PodIdentityAssociations = []controlplanev1beta2.PodIdentityAssociationSpec{
		{Namespace: "default", ServiceAccount: "app-sa", AgencyName: "app-agency"},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set pod identity associations: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.PodIdentityCreate) != 1 {
		t.Fatalf("expected 1 created association, got %d", len(fakeSvc.PodIdentityCreate))
	}

	fakeSvc.PodIdentities = []cceService.PodIdentityAssociationInfo{
		{ID: "podid-app-sa", Namespace: "default", ServiceAccount: "app-sa", AgencyName: "app-agency",
			Tags: map[string]string{cceService.OwnedTagKey("test-cluster"): "owned"}},
	}
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	latest.Spec.PodIdentityAssociations = nil
	if err := k8sClient.Update(ctx, latest); err != nil {
		t.Fatalf("failed to empty pod identity associations: %v", err)
	}
	fakeSvc.PodIdentityDelete = nil
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile (empty) returned error: %v", err)
	}
	if len(fakeSvc.PodIdentityDelete) != 1 || fakeSvc.PodIdentityDelete[0] != "podid-app-sa" {
		t.Errorf("expected cleanup delete podid-app-sa, got %v", fakeSvc.PodIdentityDelete)
	}
}

// TestControlPlaneReconcileAccessPoliciesScoped covers B4: access policies are
// account-scoped, so another cluster's policy (present in the account list)
// must never be updated or deleted by this control plane.
func TestControlPlaneReconcileAccessPoliciesScoped(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-accesspolicy-scoped"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.AccessPolicies = []controlplanev1beta2.AccessPolicySpec{
		{Name: "ours", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIds: []string{"user-1"}},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set access policies: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// The account-scoped list also contains another cluster's policy.
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-other", Name: "other-cluster-policy", ClusterIDs: []string{"cluster-other"}, PolicyType: "CCEAdminPolicy", PrincipalType: "group", PrincipalIDs: []string{"grp-9"}, Namespaces: []string{"*"}},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	// First reconcile: our policy is created; the foreign policy is untouched.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.AccessPolicyCreate) != 1 || fakeSvc.AccessPolicyCreate[0].Name != "ours" {
		t.Fatalf("expected create ours, got %+v", fakeSvc.AccessPolicyCreate)
	}
	if len(fakeSvc.AccessPolicyDelete) != 0 || len(fakeSvc.AccessPolicyUpdate) != 0 {
		t.Errorf("another cluster's policy must not be managed, delete=%v update=%v", fakeSvc.AccessPolicyDelete, fakeSvc.AccessPolicyUpdate)
	}

	// Empty the spec: our policy is removed, the foreign one survives.
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-other", Name: "other-cluster-policy", ClusterIDs: []string{"cluster-other"}, PolicyType: "CCEAdminPolicy", PrincipalType: "group", PrincipalIDs: []string{"grp-9"}, Namespaces: []string{"*"}},
		{PolicyID: "access-policy-ours", Name: "ours", ClusterIDs: []string{"cluster-1"}, PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-1"}, Namespaces: []string{"*"}},
	}
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	latest.Spec.AccessPolicies = nil
	if err := k8sClient.Update(ctx, latest); err != nil {
		t.Fatalf("failed to empty access policies: %v", err)
	}
	fakeSvc.AccessPolicyDelete = nil
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile (empty) returned error: %v", err)
	}
	if len(fakeSvc.AccessPolicyDelete) != 1 || fakeSvc.AccessPolicyDelete[0] != "access-policy-ours" {
		t.Errorf("expected only our policy deleted, got %v", fakeSvc.AccessPolicyDelete)
	}
}

// TestControlPlaneReconcileLogging verifies declarative control-plane log
// collection: apply on drift, skip when already matching.
func TestControlPlaneReconcileLogging(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-logging"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.Logging = &controlplanev1beta2.ControlPlaneLoggingSpec{
		TTLInDays: 7,
		Logs: []controlplanev1beta2.ControlPlaneLogSpec{
			{Name: "kube-apiserver", Type: "control", Enable: true},
			{Name: "audit", Type: "audit", Enable: true},
		},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to update control plane spec: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// Cloud reports a different config (audit off) -> drift must be applied.
	fakeSvc.ShowClusterLogConfigFn = func(_ context.Context, _ string) (*cceService.LogConfigInfo, error) {
		return &cceService.LogConfigInfo{
			TTLInDays: 3,
			Logs: []cceService.LogConfigInput{
				{Name: "kube-apiserver", Type: "control", Enable: true},
				{Name: "audit", Type: "audit", Enable: false},
			},
		}, nil
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.LogConfigCalls) != 1 {
		t.Fatalf("expected 1 UpdateClusterLogConfig call, got %d", len(fakeSvc.LogConfigCalls))
	}
	call := fakeSvc.LogConfigCalls[0]
	if call.ClusterID != "cluster-1" || call.TTLInDays != 7 {
		t.Errorf("unexpected log config call: %+v", call)
	}
	if len(call.Logs) != 2 || call.Logs[1].Name != "audit" || !call.Logs[1].Enable {
		t.Errorf("expected audit enabled in update, got %+v", call.Logs)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.LoggingConfiguredCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected LoggingConfigured=True, got %v", c)
	}
}

// TestControlPlaneReconcileLoggingNoDrift verifies no update when the cloud
// config already matches the spec.
func TestControlPlaneReconcileLoggingNoDrift(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-logging-nodrift"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.Logging = &controlplanev1beta2.ControlPlaneLoggingSpec{
		TTLInDays: 7,
		Logs: []controlplanev1beta2.ControlPlaneLogSpec{
			{Name: "audit", Type: "audit", Enable: true},
		},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to update control plane spec: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ShowClusterLogConfigFn = func(_ context.Context, _ string) (*cceService.LogConfigInfo, error) {
		return &cceService.LogConfigInfo{
			TTLInDays: 7,
			Logs: []cceService.LogConfigInput{
				{Name: "audit", Type: "audit", Enable: true},
			},
		}, nil
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.LogConfigCalls) != 0 {
		t.Errorf("expected no UpdateClusterLogConfig when config matches, got %d calls", len(fakeSvc.LogConfigCalls))
	}
}

// TestControlPlaneReconcileDeleteWithIdentity verifies that the delete
// path resolves credentials through spec.identityRef (it previously looked
// up only the per-cluster Secret, so identity-based clusters could never
// be deleted - the missing Secret failed the reconcile forever and the
// finalizer was never removed).
func TestControlPlaneReconcileDeleteWithIdentity(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-delete-identity"
	createNamespace(t, ns)

	createStaticIdentity(t, "cp-static-id")
	cluster, _, cp := newTestCluster(t, ns)
	// NOTE: no per-cluster credentials Secret.
	cp.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "cp-static-id"}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set identityRef: %v", err)
	}
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile (identityRef credentials) failed: %v", err)
	}

	// Trigger deletion; the delete reconcile must resolve credentials via
	// identityRef and reach DeleteCluster.
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to re-get control plane: %v", err)
	}
	if err := k8sClient.Delete(ctx, latest); err != nil {
		t.Fatalf("failed to delete control plane: %v", err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("delete reconcile must honor identityRef credentials: %v", err)
	}
	if len(fakeSvc.DeletedClusters) != 1 {
		t.Fatalf("expected 1 DeleteCluster call, got %d", len(fakeSvc.DeletedClusters))
	}
}

// TestControlPlaneReconcileRoleIdentityAgency verifies that the agency
// from a CCEClusterRoleIdentity reaches the CreateCluster input when
// spec.agencyName is unset, and that an explicit spec.agencyName wins.
func TestControlPlaneReconcileRoleIdentityAgency(t *testing.T) {
	// RoleIdentity resolves the controller credentials from env.
	t.Setenv("CLOUD_SDK_AK", "envAK")
	t.Setenv("CLOUD_SDK_SK", "envSK")
	ctx := context.Background()

	roleID := &infrav1beta2.CCEClusterRoleIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-account"},
		Spec:       infrav1beta2.CCEClusterRoleIdentitySpec{AgencyName: "delegated-agency"},
	}
	if err := k8sClient.Create(ctx, roleID); err != nil {
		t.Fatalf("failed to create CCEClusterRoleIdentity: %v", err)
	}

	// Case 1: spec.agencyName unset -> the identity agency is used.
	ns := "cp-test-agency"
	createNamespace(t, ns)
	cluster, _, cp := newTestCluster(t, ns)
	markInfrastructureProvisioned(t, cluster)
	cp.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterRoleIdentity", Name: "cross-account"}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set identityRef: %v", err)
	}
	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client:             k8sClient,
		CredentialProvider: fakes.NewFakeCredentialProvider(),
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile failed: %v", err)
	}
	if len(fakeSvc.CreatedClusters) != 1 {
		t.Fatalf("expected 1 created cluster, got %d", len(fakeSvc.CreatedClusters))
	}
	if got := fakeSvc.CreatedClusters[0].AgencyName; got != "delegated-agency" {
		t.Errorf("expected identity agency %q in create input, got %q", "delegated-agency", got)
	}

	// Case 2: explicit spec.agencyName wins over the identity agency.
	ns2 := "cp-test-agency-explicit"
	createNamespace(t, ns2)
	cluster2, _, cp2 := newTestCluster(t, ns2)
	markInfrastructureProvisioned(t, cluster2)
	cp2.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterRoleIdentity", Name: "cross-account"}
	cp2.Spec.AgencyName = "explicit-agency"
	if err := k8sClient.Update(ctx, cp2); err != nil {
		t.Fatalf("failed to set identityRef/agencyName: %v", err)
	}
	fakeSvc2 := fakes.NewFakeCCEService()
	r2 := &CCEManagedControlPlaneReconciler{
		Client:             k8sClient,
		CredentialProvider: fakes.NewFakeCredentialProvider(),
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc2, nil
		},
	}
	if _, err := r2.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp2)}); err != nil {
		t.Fatalf("initial reconcile (case 2) failed: %v", err)
	}
	if got := fakeSvc2.CreatedClusters[0].AgencyName; got != "explicit-agency" {
		t.Errorf("expected explicit spec.agencyName to win, got %q", got)
	}
}

// validTrustPolicy is a minimal IAM v5 trust-policy document (trusts the CCE
// service to assume the agency). Used by the agency auto-creation tests.
const validTrustPolicy = `{
	"Version": "5.0",
	"Statement": [{
		"Effect": "Allow",
		"Principal": {"Service": ["cce"]},
		"Action": ["sts:agencies:assume"]
	}]
}`

// TestControlPlaneReconcileAgencyAutoCreation verifies the P1-3 trust-agency
// auto-creation path: a role identity + non-empty spec.agencyTrustPolicy
// triggers EnsureAgency with the identity agency name and the policy, before
// the STS assume. An invalid policy fails the reconcile; a static identity
// (no agency) skips creation entirely.
func TestControlPlaneReconcileAgencyAutoCreation(t *testing.T) {
	t.Setenv("CLOUD_SDK_AK", "envAK")
	t.Setenv("CLOUD_SDK_SK", "envSK")
	ctx := context.Background()

	roleID := &infrav1beta2.CCEClusterRoleIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "agency-create"},
		Spec:       infrav1beta2.CCEClusterRoleIdentitySpec{AgencyName: "delegated-agency"},
	}
	if err := k8sClient.Create(ctx, roleID); err != nil {
		t.Fatalf("failed to create CCEClusterRoleIdentity: %v", err)
	}

	// Case 1: valid trust policy -> EnsureAgency called, cluster still created.
	ns := "cp-test-agency-create"
	createNamespace(t, ns)
	cluster, _, cp := newTestCluster(t, ns)
	markInfrastructureProvisioned(t, cluster)
	cp.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterRoleIdentity", Name: "agency-create"}
	cp.Spec.AgencyTrustPolicy = validTrustPolicy
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set identityRef/agencyTrustPolicy: %v", err)
	}
	fakeSvc := fakes.NewFakeCCEService()
	fakeIAM := fakes.NewFakeIAMService()
	r := &CCEManagedControlPlaneReconciler{
		Client:             k8sClient,
		CredentialProvider: fakes.NewFakeCredentialProvider(),
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
		IAMServiceFactory: func(_ string, _ *credentials.Credentials) (iamService.Service, error) {
			return fakeIAM, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile failed: %v", err)
	}
	if len(fakeIAM.EnsuredAgencies) != 1 {
		t.Fatalf("expected 1 EnsureAgency call, got %d", len(fakeIAM.EnsuredAgencies))
	}
	if got := fakeIAM.EnsuredAgencies[0].AgencyName; got != "delegated-agency" {
		t.Errorf("expected EnsureAgency agency %q, got %q", "delegated-agency", got)
	}
	if got := fakeIAM.EnsuredAgencies[0].TrustPolicy; got != validTrustPolicy {
		t.Errorf("expected EnsureAgency trustPolicy to be passed through, got %q", got)
	}
	if len(fakeSvc.CreatedClusters) != 1 {
		t.Fatalf("expected 1 created cluster after agency creation, got %d", len(fakeSvc.CreatedClusters))
	}

	// Case 2: invalid trust policy -> reconcile fails, no cluster created.
	ns2 := "cp-test-agency-bad-policy"
	createNamespace(t, ns2)
	cluster2, _, cp2 := newTestCluster(t, ns2)
	markInfrastructureProvisioned(t, cluster2)
	cp2.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterRoleIdentity", Name: "agency-create"}
	cp2.Spec.AgencyTrustPolicy = `{"Version": "1.0"}`
	if err := k8sClient.Update(ctx, cp2); err != nil {
		t.Fatalf("failed to set identityRef/bad agencyTrustPolicy: %v", err)
	}
	fakeSvc2 := fakes.NewFakeCCEService()
	fakeIAM2 := fakes.NewFakeIAMService()
	r2 := &CCEManagedControlPlaneReconciler{
		Client:             k8sClient,
		CredentialProvider: fakes.NewFakeCredentialProvider(),
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc2, nil
		},
		IAMServiceFactory: func(_ string, _ *credentials.Credentials) (iamService.Service, error) {
			return fakeIAM2, nil
		},
	}
	if _, err := r2.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp2)}); err == nil {
		t.Fatal("expected reconcile to fail on an invalid trust policy")
	}
	if len(fakeIAM2.EnsuredAgencies) != 0 {
		t.Errorf("expected no EnsureAgency call for an invalid policy, got %d", len(fakeIAM2.EnsuredAgencies))
	}
	if len(fakeSvc2.CreatedClusters) != 0 {
		t.Errorf("expected no cluster created for an invalid policy, got %d", len(fakeSvc2.CreatedClusters))
	}

	// Case 3: static identity (no agency) + trust policy -> creation skipped.
	createStaticIdentity(t, "agency-create-static")
	ns3 := "cp-test-agency-skip-static"
	createNamespace(t, ns3)
	cluster3, _, cp3 := newTestCluster(t, ns3)
	markInfrastructureProvisioned(t, cluster3)
	cp3.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "agency-create-static"}
	cp3.Spec.AgencyTrustPolicy = validTrustPolicy
	if err := k8sClient.Update(ctx, cp3); err != nil {
		t.Fatalf("failed to set static identityRef: %v", err)
	}
	fakeSvc3 := fakes.NewFakeCCEService()
	fakeIAM3 := fakes.NewFakeIAMService()
	r3 := &CCEManagedControlPlaneReconciler{
		Client:             k8sClient,
		CredentialProvider: fakes.NewFakeCredentialProvider(),
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc3, nil
		},
		IAMServiceFactory: func(_ string, _ *credentials.Credentials) (iamService.Service, error) {
			return fakeIAM3, nil
		},
	}
	if _, err := r3.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp3)}); err != nil {
		t.Fatalf("static-identity reconcile failed: %v", err)
	}
	if len(fakeIAM3.EnsuredAgencies) != 0 {
		t.Errorf("expected no EnsureAgency call for a static identity, got %d", len(fakeIAM3.EnsuredAgencies))
	}
	if len(fakeSvc3.CreatedClusters) != 1 {
		t.Fatalf("expected 1 created cluster for a static identity, got %d", len(fakeSvc3.CreatedClusters))
	}
}

// TestControlPlaneReconcileObservedGenerationUpdates verifies that after a
// successful reconcile, status.observedGeneration matches metadata.generation
// (patch.WithStatusObservedGeneration writes
// the field during Patch).
func TestControlPlaneReconcileObservedGenerationUpdates(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-obsgen-update"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	// patch.WithStatusObservedGeneration writes Status.ObservedGeneration =
	// metadata.generation during Patch. Note that the controller may also
	// modify spec (e.g. cp.Spec.ControlPlaneEndpoint backfill), which bumps
	// Generation on the server side after the patch. So we only assert
	// ObservedGeneration was actually written (>= 1).
	if got.Status.ObservedGeneration < 1 {
		t.Errorf("expected Status.ObservedGeneration >= 1 (patcher should have written it), got %d",
			got.Status.ObservedGeneration)
	}
}

// TestControlPlaneReconcileRequeueWhenObservedBehind verifies that when the
// persisted Status.ObservedGeneration is behind the spec's metadata.generation
// (a coalesced spec change), Reconcile returns RequeueAfter=defaultRequeue
// without re-running the heavy create path.
func TestControlPlaneReconcileRequeueWhenObservedBehind(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-obsgen-requeue"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	// Force the Status.ObservedGeneration to lag behind metadata.generation.
	// This simulates a coalesced spec change that arrived after our Get and
	// was not caught by the first reconcile.
	cp.Status.ObservedGeneration = 0

	// Default FakeCCEService returns an Available cluster with an Internal
	// endpoint, so the controller will try to create (cp.Status.ClusterID is
	// empty), then patch status.observedGeneration to current generation.
	// After reconcileNormal returns, the controller-level requeue check
	// compares obsAtStart (= 0) vs cp.Generation and triggers a requeue.
	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter != defaultRequeue {
		t.Errorf("expected requeue %v, got %v (Result=%v)", defaultRequeue, res.RequeueAfter, res)
	}
	if len(fakeSvc.CreatedClusters) != 1 {
		t.Errorf("expected 1 cluster creation on first reconcile, got %d", len(fakeSvc.CreatedClusters))
	}
}

// TestControlPlaneReconcileDeleteWithoutOwnerCluster covers the delete path
// when the owner Cluster reference is missing: reconcileDelete dereferences
// cluster.Spec.InfrastructureRef, so calling it with a nil cluster panicked
// (recovered by controller-runtime and requeued), leaving the control plane
// Terminating forever with its finalizer stranded. The reconcile must not
// panic, must requeue, and must keep the finalizer so deletion is retried.
func TestControlPlaneReconcileDeleteWithoutOwnerCluster(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-delete-noowner"
	createNamespace(t, ns)

	cp := &controlplanev1beta2.CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-control-plane",
			Namespace: ns,
			// Finalizer present (normally added by reconcileNormal) so the
			// delete leaves the object Terminating instead of vanishing.
			Finalizers: []string{ControlPlaneFinalizer},
		},
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName: "orphan-cluster",
			Category:    "Turbo",
			Flavor:      "cce.s2.medium",
			ContainerNetwork: controlplanev1beta2.ContainerNetworkSpec{
				Mode:       "eni",
				ENISubnets: []string{"sub-1"},
			},
			ServiceNetwork: controlplanev1beta2.ServiceNetworkSpec{CIDR: "10.247.0.0/16"},
			EndpointAccess: controlplanev1beta2.EndpointAccessSpec{Public: true},
		},
	}
	if err := k8sClient.Create(ctx, cp); err != nil {
		t.Fatalf("failed to create control plane: %v", err)
	}
	// No ownerReferences -> util.GetOwnerCluster returns (nil, nil).
	if err := k8sClient.Delete(ctx, cp); err != nil {
		t.Fatalf("failed to delete control plane: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	// A panic in Reconcile would fail the whole test process; reaching the
	// assertions below is the "does not panic" check.
	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("expected positive requeue after missing owner cluster, got %+v", res)
	}
	if len(fakeSvc.DeletedClusters) != 0 {
		t.Errorf("expected no cloud delete without an owner cluster, got %v", fakeSvc.DeletedClusters)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane after reconcile: %v", err)
	}
	if !hasFinalizer(got.Finalizers, ControlPlaneFinalizer) {
		t.Error("expected finalizer to remain so deletion is retried")
	}
}

// TestControlPlaneReconcileRejectsClusterNameMismatch covers the GC identity
// invariant: spec.clusterName must equal the owning Cluster name, otherwise
// ExternalResourceGC would treat the live CCE cluster as an orphan and delete
// it. The mismatch must be rejected before any cloud call.
func TestControlPlaneReconcileRejectsClusterNameMismatch(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-name-mismatch"
	createNamespace(t, ns)

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: ns},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1beta2.GroupVersion.Group,
				Kind:     "CCECluster",
				Name:     "test-cluster",
			},
			ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: controlplanev1beta2.GroupVersion.Group,
				Kind:     "CCEManagedControlPlane",
				Name:     "test-cluster-control-plane",
			},
		},
	}
	if err := k8sClient.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create Cluster: %v", err)
	}
	ownerRef := metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}
	cceCluster := &infrav1beta2.CCECluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cluster",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: infrav1beta2.CCEClusterSpec{
			Region: "cn-north-4",
			Network: common.NetworkSpec{
				VPC:     common.VPC{ID: "vpc-1"},
				Subnets: []common.Subnet{{ID: "sub-1"}},
			},
		},
	}
	if err := k8sClient.Create(ctx, cceCluster); err != nil {
		t.Fatalf("failed to create CCECluster: %v", err)
	}
	// clusterName is immutable after creation, so the divergent value must be
	// set here, at creation time.
	cp := &controlplanev1beta2.CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cluster-control-plane",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName: "other-cluster",
			Category:    "Turbo",
			Flavor:      "cce.s2.medium",
			ContainerNetwork: controlplanev1beta2.ContainerNetworkSpec{
				Mode:       "eni",
				ENISubnets: []string{"sub-1"},
			},
			ServiceNetwork: controlplanev1beta2.ServiceNetworkSpec{CIDR: "10.247.0.0/16"},
			EndpointAccess: controlplanev1beta2.EndpointAccessSpec{Public: true},
		},
	}
	if err := k8sClient.Create(ctx, cp); err != nil {
		t.Fatalf("failed to create control plane: %v", err)
	}
	createCredentialsSecret(t, ns, "test-cluster")
	createCredentialsSecret(t, ns, "other-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.CreatedClusters) != 0 {
		t.Errorf("expected no cluster creation on name mismatch, got %v", fakeSvc.CreatedClusters)
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	c := capiconditions.Get(got, conditions.CCEClusterReadyCondition)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != conditions.CCEClusterNameMismatchReason {
		t.Errorf("expected %s=False reason %s, got %v",
			conditions.CCEClusterReadyCondition, conditions.CCEClusterNameMismatchReason, c)
	}
}

// TestToCreateClusterInputDataPlaneV2 covers T2b: the controller-side mapping
// of spec.enableDataPlaneV2 into the CreateCluster input (nil -> false, true ->
// true, false -> false). Only the webhook immutability was covered before.
func TestToCreateClusterInputDataPlaneV2(t *testing.T) {
	cases := []struct {
		name string
		spec *bool
		want bool
	}{
		{"nil defaults to false", nil, false},
		{"true maps to true", boolPtr(true), true},
		{"explicit false maps to false", boolPtr(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := &controlplanev1beta2.CCEManagedControlPlane{
				Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{EnableDataPlaneV2: tc.spec},
			}
			in := toCreateClusterInput(cp, "vpc-1", "subnet-1", "", nil)
			if in.EnableDataPlaneV2 != tc.want {
				t.Errorf("EnableDataPlaneV2 = %v, want %v", in.EnableDataPlaneV2, tc.want)
			}
		})
	}
}

// TestControlPlaneReconcileDeleteReissuesOnlyWhileDeleting covers B11 + H3:
// the delete path issues DeleteCluster once, then polls ShowCluster while the
// platform reports the cluster as Deleting (the keep-alive annotation makes the
// watch re-drive the reconcile even if the delayed requeue is coalesced away).
// If the cluster is not in the Deleting phase — the async delete failed or the
// cluster fell back — the request is re-issued instead of being suppressed
// forever by the annotation.
func TestControlPlaneReconcileDeleteReissuesOnlyWhileDeleting(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-delete-once"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	// Model the real platform: the phase only becomes Deleting once
	// DeleteCluster has been accepted (and can fall back afterwards).
	phase := "Available"
	fakeSvc.ShowClusterFn = func(_ context.Context, clusterID string) (*cceService.ClusterInfo, error) {
		return &cceService.ClusterInfo{
			ClusterID: clusterID,
			Phase:     phase,
			Version:   "v1.30.0",
			Endpoints: []cceService.Endpoint{{URL: "https://10.0.0.10:5443", Type: "Internal"}},
		}, nil
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	// Provision: sets Status.ClusterID and the finalizer.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile failed: %v", err)
	}

	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to re-get control plane: %v", err)
	}
	if err := k8sClient.Delete(ctx, latest); err != nil {
		t.Fatalf("failed to delete control plane: %v", err)
	}

	// First delete reconcile: DeleteCluster once + keep-alive annotation.
	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	if err != nil {
		t.Fatalf("first delete reconcile failed: %v", err)
	}
	if len(fakeSvc.DeletedClusters) != 1 {
		t.Fatalf("expected 1 DeleteCluster call, got %d", len(fakeSvc.DeletedClusters))
	}
	if res.RequeueAfter <= 0 {
		t.Errorf("expected a positive requeue while the cluster is deleting, got %+v", res)
	}
	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if got.Annotations[controlPlaneDeletePollAnnotation] == "" {
		t.Fatal("expected the keep-alive delete poll annotation to be stamped")
	}
	// The platform now reports Deleting: the delete request must not be
	// re-issued while the deletion is genuinely in progress.
	phase = "Deleting"
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("second delete reconcile failed: %v", err)
	}
	if len(fakeSvc.DeletedClusters) != 1 {
		t.Errorf("DeleteCluster must not be re-issued while Deleting, got %d calls", len(fakeSvc.DeletedClusters))
	}

	// H3: the cluster falls back to a non-deleting phase (async delete failed).
	// The annotation alone must not suppress the retry forever, or the
	// finalizer is stranded and the CCE cluster keeps billing.
	phase = "Available"
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("third delete reconcile failed: %v", err)
	}
	if len(fakeSvc.DeletedClusters) != 2 {
		t.Errorf("DeleteCluster must be re-issued once the phase leaves Deleting, got %d calls", len(fakeSvc.DeletedClusters))
	}
}

// TestControlPlaneReconcilePostAvailableThrottleBacksOff covers B12: a throttled
// post-Available write (ReconcileClusterTags) must be converted into a tuned
// requeue with no error, instead of a raw error that lets controller-runtime's
// fast exponential backoff hammer the API.
func TestControlPlaneReconcilePostAvailableThrottleBacksOff(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-postavailable-throttle"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	key := client.ObjectKeyFromObject(cp)
	resetBackoff(key)
	defer resetBackoff(key)

	// Pre-seed an already-provisioned, steady-state status so this reconcile
	// exercises the post-Available path (no create). observedGeneration is
	// deliberately left behind generation: H2 — the observed-generation requeue
	// must not override the tuned throttle backoff.
	seed := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, key, seed); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	seed.Status.ClusterID = "cluster-1"
	seed.Status.ControlPlaneEndpoint = &clusterv1.APIEndpoint{Host: "10.0.0.10", Port: 5443}
	if err := k8sClient.Status().Update(ctx, seed); err != nil {
		t.Fatalf("failed to seed control plane status: %v", err)
	}
	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ReconcileClusterTagsFn = func(_ context.Context, _, _ string, _ map[string]string) error {
		return &sdkerr.ServiceResponseError{StatusCode: 429}
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatalf("throttled post-Available write must not surface as an error: %v", err)
	}
	if res.RequeueAfter != throttledBackoffBase {
		t.Errorf("throttled post-Available write: RequeueAfter = %v, want %v", res.RequeueAfter, throttledBackoffBase)
	}
}

// TestControlPlaneReconcileResetsBackoffOnSuccess covers B14: the steady-state
// success returns RequeueAfter: reconciliationPeriod (not 0), so the old
// RequeueAfter==0 gate never reset the failure counter. A clean reconcile must
// clear a stale capped counter left by an earlier throttle burst.
func TestControlPlaneReconcileResetsBackoffOnSuccess(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-backoff-reset"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	key := client.ObjectKeyFromObject(cp)
	resetBackoff(key)
	defer resetBackoff(key)
	// Simulate a burst of create-time throttles (counter climbs to the cap).
	for i := 0; i < 6; i++ {
		requeueAfterForError(key, &sdkerr.ServiceResponseError{StatusCode: 429})
	}
	if got := errorBackoff.failures(key); got == 0 {
		t.Fatal("expected the primed failure counter to be non-zero")
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("clean reconcile failed: %v", err)
	}
	if got := errorBackoff.failures(key); got != 0 {
		t.Errorf("clean reconcile must reset the backoff counter, got %d", got)
	}
}

// TestControlPlaneReconcilePodIdentityForeignNotAdopted covers the ownership
// boundary for a collision: a declared service account whose association
// already exists but is not owned by the provider must be neither adopted (CCE
// cannot add tags after create) nor deleted.
func TestControlPlaneReconcilePodIdentityForeignNotAdopted(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-podid-foreign"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.PodIdentityAssociations = []controlplanev1beta2.PodIdentityAssociationSpec{
		{Namespace: "default", ServiceAccount: "app-sa", AgencyName: "app-agency"},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to update control plane spec: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// Same namespace/service account, but no owned tag: foreign.
	fakeSvc.PodIdentities = []cceService.PodIdentityAssociationInfo{
		{ID: "podid-foreign", Namespace: "default", ServiceAccount: "app-sa", AgencyName: "other-agency"},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.PodIdentityCreate) != 0 {
		t.Errorf("a foreign association must not be adopted (no create), got %+v", fakeSvc.PodIdentityCreate)
	}
	if len(fakeSvc.PodIdentityDelete) != 0 {
		t.Errorf("a foreign association must never be deleted, got %v", fakeSvc.PodIdentityDelete)
	}
}

// TestControlPlaneReconcileAccessPolicyForeignCollision covers M1: a declared
// policy whose name resolves to an in-scope policy this provider does not own
// cannot be applied. The reconcile must fail loudly (condition False) rather
// than skip it while reporting the condition True.
func TestControlPlaneReconcileAccessPolicyForeignCollision(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-accesspolicy-collision"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.AccessPolicies = []controlplanev1beta2.AccessPolicySpec{
		{Name: "shared", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIds: []string{"user-1"}},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set access policies: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// Same name, in scope (this cluster), but not in status.accessPolicies.
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-foreign", Name: "shared", ClusterIDs: []string{"cluster-1"}, PolicyType: "CCEAdminPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-9"}, Namespaces: []string{"*"}},
	}
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err == nil {
		t.Fatal("expected the reconcile to fail on a foreign same-name policy instead of skipping silently")
	}
	if len(fakeSvc.AccessPolicyCreate) != 0 || len(fakeSvc.AccessPolicyUpdate) != 0 || len(fakeSvc.AccessPolicyDelete) != 0 {
		t.Errorf("a foreign policy must not be created/updated/deleted, create=%v update=%v delete=%v",
			fakeSvc.AccessPolicyCreate, fakeSvc.AccessPolicyUpdate, fakeSvc.AccessPolicyDelete)
	}
	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.AccessPoliciesConfiguredCondition); c == nil || c.Status != metav1.ConditionFalse {
		t.Errorf("expected AccessPoliciesConfigured=False on the collision, got %v", c)
	}
}
