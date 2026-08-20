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
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
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

// TestControlPlaneReconcilePodIdentity verifies declarative pod-identity
// association management: create missing, delete removed.
func TestControlPlaneReconcilePodIdentity(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-podid"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	cp.Spec.PodIdentityAssociations = []controlplanev1beta1.PodIdentityAssociationSpec{
		{Namespace: "default", ServiceAccount: "app-sa", AgencyName: "app-agency"},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to update control plane spec: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	// Cloud already has one association that is no longer in spec -> delete.
	fakeSvc.PodIdentities = []cceService.PodIdentityAssociationInfo{
		{ID: "podid-old", Namespace: "kube-system", ServiceAccount: "old-sa", AgencyName: "old-agency"},
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

	if len(fakeSvc.PodIdentityCreate) != 1 {
		t.Fatalf("expected 1 create, got %d", len(fakeSvc.PodIdentityCreate))
	}
	created := fakeSvc.PodIdentityCreate[0]
	if created.Namespace != "default" || created.ServiceAccount != "app-sa" || created.AgencyName != "app-agency" {
		t.Errorf("unexpected create input: %+v", created)
	}
	if len(fakeSvc.PodIdentityDelete) != 1 || fakeSvc.PodIdentityDelete[0] != "podid-old" {
		t.Errorf("expected delete podid-old, got %v", fakeSvc.PodIdentityDelete)
	}

	got := &controlplanev1beta1.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.PodIdentityAssociationsConfiguredCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected PodIdentityAssociationsConfigured=True, got %v", c)
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

	cp.Spec.Logging = &controlplanev1beta1.ControlPlaneLoggingSpec{
		TTLInDays: 7,
		Logs: []controlplanev1beta1.ControlPlaneLogSpec{
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
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

	got := &controlplanev1beta1.CCEManagedControlPlane{}
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

	cp.Spec.Logging = &controlplanev1beta1.ControlPlaneLoggingSpec{
		TTLInDays: 7,
		Logs: []controlplanev1beta1.ControlPlaneLogSpec{
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
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
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("initial reconcile (identityRef credentials) failed: %v", err)
	}

	// Trigger deletion; the delete reconcile must resolve credentials via
	// identityRef and reach DeleteCluster.
	latest := &controlplanev1beta1.CCEManagedControlPlane{}
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

	roleID := &infrav1beta1.CCEClusterRoleIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-account"},
		Spec:       infrav1beta1.CCEClusterRoleIdentitySpec{AgencyName: "delegated-agency"},
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
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
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
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
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
