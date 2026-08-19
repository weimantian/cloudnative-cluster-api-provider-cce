/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &controlplanev1beta1.CCEManagedControlPlane{}
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
	if fakeSvc.KubeconfigCalls != 1 {
		t.Errorf("expected 1 kubeconfig call, got %d", fakeSvc.KubeconfigCalls)
	}
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "test-cluster-kubeconfig"}, secret); err != nil {
		t.Fatalf("expected kubeconfig Secret: %v", err)
	}
	if len(secret.Data["value"]) == 0 {
		t.Error("expected kubeconfig Secret data")
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	// Make it ready first so Status.ClusterID is set and deletion has an ID.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile failed: %v", err)
	}

	// Trigger deletion via Delete() (deletionTimestamp is set by the API
	// server; the object survives because the controller added a finalizer).
	latest := &controlplanev1beta1.CCEManagedControlPlane{}
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
func upgradeCP(t *testing.T, ns string) (*clusterv1.Cluster, *controlplanev1beta1.CCEManagedControlPlane, *fakes.FakeCCEService, *CCEManagedControlPlaneReconciler) {
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
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
	got := &controlplanev1beta1.CCEManagedControlPlane{}
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
	got := &controlplanev1beta1.CCEManagedControlPlane{}
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
	got := &controlplanev1beta1.CCEManagedControlPlane{}
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
// create missing, upgrade version drift, delete those no longer listed.
func TestControlPlaneReconcileAddons(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-addons"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	// Declare two addons; one already on the cloud at a stale version.
	cp.Spec.Addons = []controlplanev1beta1.AddonSpec{
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// metrics-server must be created, coredns upgraded (drift), old-addon deleted.
	if len(fakeSvc.AddonCreateCalls) != 1 || fakeSvc.AddonCreateCalls[0].Name != "metrics-server" {
		t.Errorf("expected create metrics-server, got %+v", fakeSvc.AddonCreateCalls)
	}
	if len(fakeSvc.AddonUpdateCalls) != 1 || fakeSvc.AddonUpdateCalls[0].Name != "coredns" || fakeSvc.AddonUpdateCalls[0].Version != "1.2.0" {
		t.Errorf("expected upgrade coredns to 1.2.0, got %+v", fakeSvc.AddonUpdateCalls)
	}
	if len(fakeSvc.AddonDeleteCalls) != 1 || fakeSvc.AddonDeleteCalls[0] != "addon-id-old" {
		t.Errorf("expected delete old-addon, got %v", fakeSvc.AddonDeleteCalls)
	}

	got := &controlplanev1beta1.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.AddonsConfiguredCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected AddonsConfigured=True, got %v", c)
	}
}
