/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

// TestMachinePoolReconcileAutoscalingGate verifies B3: autoscaling is only
// mapped to the CCE API when the NodePoolAutoscaling feature gate is on.
func TestMachinePoolReconcileAutoscalingGate(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-autoscale"
	createNamespace(t, ns)

	setupPoolReconciler := func() (*fakes.FakeCCEService, *CCEManagedMachinePoolReconciler, *infrav1beta2.CCEManagedMachinePool) {
		cluster, _, cp := newTestCluster(t, ns)
		createCredentialsSecret(t, ns, "test-cluster")
		markInfrastructureProvisioned(t, cluster)
		cp.Status.ClusterID = "cluster-1"
		cp.Status.Ready = true
		if err := k8sClient.Status().Update(ctx, cp); err != nil {
			t.Fatalf("failed to set control plane status: %v", err)
		}
		mp := &clusterv1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-pool-0", Namespace: ns},
			Spec: clusterv1.MachinePoolSpec{
				ClusterName: "test-cluster",
				Replicas:    int32Ptr(3),
				Template: clusterv1.MachineTemplateSpec{
					Spec: clusterv1.MachineSpec{
						ClusterName: "test-cluster",
						Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
						InfrastructureRef: clusterv1.ContractVersionedObjectReference{
							APIGroup: infrav1beta2.GroupVersion.Group,
							Kind:     "CCEManagedMachinePool",
							Name:     "test-cluster-pool-0",
						},
					},
				},
			},
		}
		if err := k8sClient.Create(ctx, mp); err != nil {
			t.Fatalf("failed to create MachinePool: %v", err)
		}
		pool := &infrav1beta2.CCEManagedMachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster-pool-0",
				Namespace: ns,
				Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
			},
			Spec: infrav1beta2.CCEManagedMachinePoolSpec{
				ClusterName:  "test-cluster",
				NodePoolName: "pool-0",
				Flavor:       "c7.large.2",
				Replicas:     3,
				Autoscaling:  infrav1beta2.AutoscalingSpec{Enable: true, MinNodeCount: 1, MaxNodeCount: 5},
			},
		}
		if err := k8sClient.Create(ctx, pool); err != nil {
			t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
		}
		fakeSvc := fakes.NewFakeCCEService()
		r := &CCEManagedMachinePoolReconciler{
			Client: k8sClient,
			ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
				return fakeSvc, nil
			},
		}
		return fakeSvc, r, pool
	}

	// Gate OFF: autoscaling must NOT be sent to the cloud.
	if err := features.SetFromMap(map[string]bool{string(features.NodePoolAutoscaling): false}); err != nil {
		t.Fatalf("disable gate: %v", err)
	}
	fakeSvc, r, pool := setupPoolReconciler()
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("Reconcile (gate off) returned error: %v", err)
	}
	if got := fakeSvc.CreatedNodePools[0].Autoscaling; got != nil {
		t.Errorf("expected autoscaling nil when gate off, got %+v", got)
	}

	// Gate ON: autoscaling must be mapped (enable/min/max).
	if err := features.SetFromMap(map[string]bool{string(features.NodePoolAutoscaling): true}); err != nil {
		t.Fatalf("enable gate: %v", err)
	}
	defer func() {
		_ = features.SetFromMap(map[string]bool{string(features.NodePoolAutoscaling): false})
	}()
	ns2 := ns + "-on"
	createNamespace(t, ns2)
	cluster2, _, cp2 := newTestCluster(t, ns2)
	createCredentialsSecret(t, ns2, "test-cluster")
	markInfrastructureProvisioned(t, cluster2)
	cp2.Status.ClusterID = "cluster-1"
	cp2.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp2); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}
	mp2 := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-pool-0", Namespace: ns2},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(3),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp2); err != nil {
		t.Fatalf("failed to create MachinePool (gate on): %v", err)
	}
	pool2 := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns2,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
			Autoscaling:  infrav1beta2.AutoscalingSpec{Enable: true, MinNodeCount: 1, MaxNodeCount: 5},
		},
	}
	if err := k8sClient.Create(ctx, pool2); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool (gate on): %v", err)
	}
	fakeSvc2 := fakes.NewFakeCCEService()
	r2 := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc2, nil
		},
	}
	if _, err := r2.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool2)}); err != nil {
		t.Fatalf("Reconcile (gate on) returned error: %v", err)
	}
	a := fakeSvc2.CreatedNodePools[0].Autoscaling
	if a == nil || !a.Enable || a.MinNodeCount != 1 || a.MaxNodeCount != 5 {
		t.Errorf("expected autoscaling {true,1,5} when gate on, got %+v", a)
	}
}

func TestMachinePoolReconcileSuccess(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-success"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	// Mark the control plane ready with a cluster ID (as the CP controller
	// would after a successful reconcile).
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	// Create the MachinePool (CAPI) and its infrastructure object.
	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-pool-0", Namespace: ns},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(3),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: stringPtr(""),
					},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &infrav1beta2.CCEManagedMachinePool{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), got); err != nil {
		t.Fatalf("failed to get machine pool: %v", err)
	}
	if !got.Status.Ready {
		t.Error("expected machine pool Ready")
	}
	if got.Status.NodePoolID != "nodepool-1" {
		t.Errorf("expected NodePoolID nodepool-1, got %q", got.Status.NodePoolID)
	}
	if c := capiconditions.Get(got, conditions.NodePoolReadyCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected NodePoolReady=True, got %v", c)
	}

	// The node pool was created with the absolute target count (replicas),
	// not a delta (questionnaire Q3 absolute semantics), so no redundant
	// ScaleNodePool should follow — the platform rejects it with
	// "No scale task needed" (verified live).
	if len(fakeSvc.CreatedNodePools) != 1 {
		t.Fatalf("expected 1 created node pool, got %d", len(fakeSvc.CreatedNodePools))
	}
	if fakeSvc.CreatedNodePools[0].ClusterID != "cluster-1" || fakeSvc.CreatedNodePools[0].InitialNodeCount != 3 {
		t.Errorf("unexpected create node pool input: %+v", fakeSvc.CreatedNodePools[0])
	}
	if len(fakeSvc.ScaleCalls) != 0 {
		t.Errorf("expected no ScaleNodePool right after create (initialNodeCount already set the target), got %v", fakeSvc.ScaleCalls)
	}
}

// TestMachinePoolReconcileSecurityGroupDrift verifies B1b: when the pool's
// replicas are already aligned but a mutable attribute (security groups)
// drifts, the controller issues UpdateNodePool with IgnoreInitialNodeCount=true
// so the attribute sync never rescales the pool (questionnaire Q3/Q5).
func TestMachinePoolReconcileSecurityGroupDrift(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-sgdrift"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-pool-0", Namespace: ns},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(3),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:    "test-cluster",
			NodePoolName:   "pool-0",
			Flavor:         "c7.large.2",
			Replicas:       3,
			SecurityGroups: []string{"sg-1"},
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	// First reconcile: create pool, scale to replicas. The create already
	// bound sg-1, so no UpdateNodePool should fire.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}
	if len(fakeSvc.UpdateNodePoolCalls) != 0 {
		t.Fatalf("expected no UpdateNodePool after create, got %d calls", len(fakeSvc.UpdateNodePoolCalls))
	}

	// Drift the security groups, then reconcile again.
	got := &infrav1beta2.CCEManagedMachinePool{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), got); err != nil {
		t.Fatalf("failed to get machine pool: %v", err)
	}
	got.Spec.SecurityGroups = []string{"sg-2"}
	if err := k8sClient.Update(ctx, got); err != nil {
		t.Fatalf("failed to update machine pool: %v", err)
	}

	scaleCallsBefore := len(fakeSvc.ScaleCalls)
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(got)}); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}

	// No rescale: replicas were already aligned (3 == 3).
	if len(fakeSvc.ScaleCalls) != scaleCallsBefore {
		t.Errorf("expected no ScaleNodePool on attribute drift, got calls %v", fakeSvc.ScaleCalls)
	}
	// One attribute update, with IgnoreInitialNodeCount=true (never shrink).
	if len(fakeSvc.UpdateNodePoolCalls) != 1 {
		t.Fatalf("expected 1 UpdateNodePool call, got %d", len(fakeSvc.UpdateNodePoolCalls))
	}
	u := fakeSvc.UpdateNodePoolCalls[0]
	if !u.IgnoreInitialNodeCount {
		t.Error("expected IgnoreInitialNodeCount=true to prevent accidental shrink (Q3)")
	}
	if len(u.CustomSecurityGroups) != 1 || u.CustomSecurityGroups[0] != "sg-2" {
		t.Errorf("expected CustomSecurityGroups [sg-2], got %v", u.CustomSecurityGroups)
	}
	// The attribute drift must also trigger a rolling update of existing nodes
	// (UpgradeNodePool), defaulted to maxUnavailable=1 when unspecified.
	if len(fakeSvc.UpgradeNodePoolCalls) != 1 {
		t.Fatalf("expected 1 UpgradeNodePool call, got %d", len(fakeSvc.UpgradeNodePoolCalls))
	}
	uc := fakeSvc.UpgradeNodePoolCalls[0]
	if uc.NodePoolID != "nodepool-1" || uc.ClusterID != "cluster-1" {
		t.Errorf("unexpected UpgradeNodePool target: %+v", uc)
	}
	if uc.MaxUnavailable != 1 {
		t.Errorf("expected default MaxUnavailable=1, got %d", uc.MaxUnavailable)
	}

	// A third reconcile with no changes must not call UpdateNodePool again.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(got)}); err != nil {
		t.Fatalf("third Reconcile returned error: %v", err)
	}
	if len(fakeSvc.UpdateNodePoolCalls) != 1 {
		t.Errorf("expected no extra UpdateNodePool when nothing drifted, got %d calls", len(fakeSvc.UpdateNodePoolCalls))
	}
	if len(fakeSvc.UpgradeNodePoolCalls) != 1 {
		t.Errorf("expected no extra UpgradeNodePool when nothing drifted, got %d calls", len(fakeSvc.UpgradeNodePoolCalls))
	}
}

func TestMachinePoolReconcileWaitsForControlPlane(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-waiting"
	createNamespace(t, ns)

	cluster, _, _ := newTestCluster(t, ns)
	markInfrastructureProvisioned(t, cluster)
	// Control plane NOT ready.
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create machine pool: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter != defaultRequeue {
		t.Errorf("expected requeue %v, got %v", defaultRequeue, res.RequeueAfter)
	}
	if len(fakeSvc.CreatedNodePools) != 0 {
		t.Error("expected no node pool creation while control plane not ready")
	}
}

func int32Ptr(i int32) *int32 { return &i }

func stringPtr(s string) *string { return &s }

// TestMachinePoolReconcileDelete exercises the full deletion path: keeps
// requesting deletion while the pool still exists, then clears the ID and
// removes the finalizer once it is gone (the earlier bug left the finalizer
// forever).
func TestMachinePoolReconcileDelete(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-delete"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster-pool-0",
			Namespace:  ns,
			Labels:     map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
			Finalizers: []string{MachinePoolFinalizer},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create machine pool: %v", err)
	}
	// Set the status AFTER Create (Create round-trips the object and would
	// otherwise overwrite the in-memory status with an empty one).
	pool.Status.NodePoolID = "nodepool-1"
	if err := k8sClient.Status().Update(ctx, pool); err != nil {
		t.Fatalf("failed to set pool status: %v", err)
	}

	deleteCalls := 0
	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.DeleteNodePoolFn = func(_ context.Context, _, _ string) error {
		deleteCalls++
		return nil
	}
	// First pass: the pool still exists -> DeleteNodePool, requeue, keep ID.
	fakeSvc.ListNodePoolsFn = func(_ context.Context, _ string) ([]cceService.NodePoolInfo, error) {
		return []cceService.NodePoolInfo{{NodePoolID: "nodepool-1", Name: "pool-0", NodeCount: 3, ActiveNodeCount: 3}}, nil
	}
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	res, err := r.reconcileDelete(ctx, cluster, pool)
	if err != nil {
		t.Fatalf("first reconcileDelete returned error: %v", err)
	}
	if res.RequeueAfter != defaultRequeue {
		t.Errorf("expected requeue while pool still exists, got %v", res.RequeueAfter)
	}
	if deleteCalls != 1 {
		t.Errorf("expected 1 DeleteNodePool call, got %d", deleteCalls)
	}
	if pool.Status.NodePoolID == "" {
		t.Error("NodePoolID must be kept while the pool still exists")
	}

	// Second pass: the pool is gone -> clear ID, remove finalizer.
	fakeSvc.ListNodePoolsFn = func(_ context.Context, _ string) ([]cceService.NodePoolInfo, error) {
		return []cceService.NodePoolInfo{}, nil
	}
	if _, err := r.reconcileDelete(ctx, cluster, pool); err != nil {
		t.Fatalf("second reconcileDelete returned error: %v", err)
	}
	if pool.Status.NodePoolID != "" {
		t.Errorf("expected NodePoolID cleared, got %q", pool.Status.NodePoolID)
	}
	if hasFinalizer(pool.Finalizers, MachinePoolFinalizer) {
		t.Error("expected finalizer removed after pool deletion")
	}
}

// TestControlPlaneReconcileCredentialsFailure verifies that a missing
// credentials Secret surfaces as CredentialsReady=False (persisted), not a
// silent env fallback.
func TestControlPlaneReconcileCredentialsFailure(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-credsfail"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	markInfrastructureProvisioned(t, cluster)
	// NOTE: no credentials Secret is created.

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err == nil {
		t.Fatal("expected Reconcile to fail when credentials Secret is missing")
	}

	got := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), got); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if c := capiconditions.Get(got, conditions.CredentialsReadyCondition); c == nil || c.Status != metav1.ConditionFalse {
		t.Errorf("expected CredentialsReady=False persisted, got %v", c)
	}
	if len(fakeSvc.CreatedClusters) != 0 {
		t.Error("no cluster must be created when credentials are missing")
	}
}

// TestMachinePoolReconcileSyncsReplicasFromOwner verifies that
// spec.replicas is copied from the owning CAPI MachinePool, so
// `kubectl scale machinepool` drives the CCE node pool size.
func TestMachinePoolReconcileSyncsReplicasFromOwner(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-syncreplicas"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(5),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3, // stale — must be synced to 5 from the MachinePool
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &infrav1beta2.CCEManagedMachinePool{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), got); err != nil {
		t.Fatalf("failed to get machine pool: %v", err)
	}
	if got.Spec.Replicas != 5 {
		t.Errorf("expected spec.replicas synced to 5, got %d", got.Spec.Replicas)
	}
	// The create already used InitialNodeCount from the (stale) spec, but the
	// reconcile should have scaled to the owner's 5.
	if len(fakeSvc.ScaleCalls) == 0 || fakeSvc.ScaleCalls[len(fakeSvc.ScaleCalls)-1] != 5 {
		t.Errorf("expected a ScaleNodePool(5) call, got %v", fakeSvc.ScaleCalls)
	}
}

// TestMachinePoolToInfraPoolMapper verifies the event mapper used by the
// MachinePool watch: CCE infra refs map to the referenced pool in the same
// namespace; foreign refs, empty names and non-MachinePool objects are
// ignored.
func TestMachinePoolToInfraPoolMapper(t *testing.T) {
	r := &CCEManagedMachinePoolReconciler{}

	mp := func(ref clusterv1.ContractVersionedObjectReference) *clusterv1.MachinePool {
		return &clusterv1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{Name: "mp", Namespace: "ns"},
			Spec: clusterv1.MachinePoolSpec{
				Template: clusterv1.MachineTemplateSpec{
					Spec: clusterv1.MachineSpec{InfrastructureRef: ref},
				},
			},
		}
	}

	got := r.machinePoolToInfraPool(context.Background(), mp(clusterv1.ContractVersionedObjectReference{
		Kind: "CCEManagedMachinePool", Name: "pool-0",
	}))
	if len(got) != 1 || got[0].Namespace != "ns" || got[0].Name != "pool-0" {
		t.Errorf("unexpected mapping: %+v", got)
	}

	got = r.machinePoolToInfraPool(context.Background(), mp(clusterv1.ContractVersionedObjectReference{
		Kind: "AWSManagedMachinePool", Name: "pool-0",
	}))
	if len(got) != 0 {
		t.Errorf("expected no mapping for foreign infra ref, got %+v", got)
	}

	got = r.machinePoolToInfraPool(context.Background(), mp(clusterv1.ContractVersionedObjectReference{
		Kind: "CCEManagedMachinePool",
	}))
	if len(got) != 0 {
		t.Errorf("expected no mapping for empty ref name, got %+v", got)
	}

	got = r.machinePoolToInfraPool(context.Background(), &infrav1beta2.CCEManagedMachinePool{})
	if len(got) != 0 {
		t.Errorf("expected no mapping for non-MachinePool object, got %+v", got)
	}
}

// TestMachinePoolScaleTriggeredByWatch is the regression test for the
// scale trigger gap: the controller must watch the CAPI MachinePool so
// that `kubectl scale machinepool` (a spec.replicas update) reconciles the
// infrastructure pool. CAPI core does NOT sync replicas onto infra pools,
// so without this watch the update has no trigger path. The test runs a
// real manager and never calls Reconcile directly.
func TestMachinePoolScaleTriggeredByWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ns := "mp-test-watch"
	createNamespace(t, ns)

	// Objects first, manager second: the informer start then emits Add
	// events for the existing pool, which drives the initial reconcile.
	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}
	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(3),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = infrav1beta2.AddToScheme(scheme)
	_ = controlplanev1beta2.AddToScheme(scheme)
	mgr, err := ctrl.NewManager(restCfg, manager.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedMachinePoolReconciler{
		Client: mgr.GetClient(),
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if err := r.SetupWithManager(ctx, mgr, controller.Options{}); err != nil {
		t.Fatalf("SetupWithManager failed: %v", err)
	}
	go func() { _ = mgr.Start(ctx) }()
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("cache failed to sync")
	}

	// Initial reconcile is event-driven (For() watch on the pool itself).
	if !waitForCondition(t, 30*time.Second, func() bool {
		got := &infrav1beta2.CCEManagedMachinePool{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), got); err != nil {
			return false
		}
		return got.Status.NodePoolID == "nodepool-1" && got.Status.Ready
	}) {
		t.Fatal("initial reconcile did not create the node pool")
	}

	// Scale the CAPI MachinePool 3 -> 5 (what kubectl scale does).
	latest := &clusterv1.MachinePool{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mp), latest); err != nil {
		t.Fatalf("failed to get MachinePool: %v", err)
	}
	replicas := int32(5)
	latest.Spec.Replicas = &replicas
	if err := k8sClient.Update(ctx, latest); err != nil {
		t.Fatalf("failed to scale MachinePool: %v", err)
	}

	// The MachinePool watch must reconcile the infra pool: spec.replicas
	// syncs from the owner and the pool scales to 5. Without the watch this
	// never happens (no other event touches the pool).
	if !waitForCondition(t, 30*time.Second, func() bool {
		got := &infrav1beta2.CCEManagedMachinePool{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), got); err != nil {
			return false
		}
		return got.Spec.Replicas == 5
	}) {
		t.Fatal("scale was not propagated: spec.replicas never synced to 5 (MachinePool watch missing?)")
	}
	if len(fakeSvc.ScaleCalls) == 0 || fakeSvc.ScaleCalls[len(fakeSvc.ScaleCalls)-1] != 5 {
		t.Errorf("expected a ScaleNodePool(5) call, got %v", fakeSvc.ScaleCalls)
	}
}

// TestMachinePoolReconcileWithIdentity verifies that the machine pool
// controller resolves credentials through the control plane's identityRef
// (it previously looked up only the per-cluster Secret, so pools of
// identity-based clusters never reconciled).
func TestMachinePoolReconcileWithIdentity(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-identity"
	createNamespace(t, ns)

	createStaticIdentity(t, "mp-static-id")
	cluster, _, cp := newTestCluster(t, ns)
	// NOTE: no per-cluster credentials Secret.
	cp.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "mp-static-id"}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set identityRef: %v", err)
	}
	markInfrastructureProvisioned(t, cluster)
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(3),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("Reconcile must resolve credentials via identityRef: %v", err)
	}
	if len(fakeSvc.CreatedNodePools) != 1 {
		t.Errorf("expected node pool created via identity credentials, got %d", len(fakeSvc.CreatedNodePools))
	}
}

// TestMachinePoolReconcileDeleteControlPlaneGone verifies that deleting
// the infra pool after the control plane is already gone releases the
// finalizer instead of erroring forever on the control plane NotFound
// (the control plane finalizer guarantees the cloud cluster - and with it
// the node pool - was deleted first).
func TestMachinePoolReconcileDeleteControlPlaneGone(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-delete-cpgone"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	if err := k8sClient.Delete(ctx, cp); err != nil {
		t.Fatalf("failed to delete control plane: %v", err)
	}

	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster-pool-0",
			Namespace:  ns,
			Labels:     map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
			Finalizers: []string{MachinePoolFinalizer},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create machine pool: %v", err)
	}
	pool.Status.NodePoolID = "nodepool-1"
	if err := k8sClient.Status().Update(ctx, pool); err != nil {
		t.Fatalf("failed to set pool status: %v", err)
	}

	listCalls := 0
	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ListNodePoolsFn = func(_ context.Context, _ string) ([]cceService.NodePoolInfo, error) {
		listCalls++
		return []cceService.NodePoolInfo{}, nil
	}
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.reconcileDelete(ctx, cluster, pool); err != nil {
		t.Fatalf("reconcileDelete must release when the control plane is gone: %v", err)
	}
	if listCalls != 0 {
		t.Errorf("expected no cloud calls when the control plane is gone, got %d", listCalls)
	}
	if pool.Status.NodePoolID != "" {
		t.Errorf("expected NodePoolID cleared, got %q", pool.Status.NodePoolID)
	}
	if hasFinalizer(pool.Finalizers, MachinePoolFinalizer) {
		t.Error("expected finalizer removed")
	}
}

// TestMachinePoolReconcileReplicasExternallyManaged verifies the CAPA-parity
// reverse-sync: when the owning MachinePool carries the external-autoscaler
// annotation, the provider does NOT scale, and instead patches the CAPI
// MachinePool.spec.replicas to the cloud-side node count (mirrors CAPA
// eks/nodegroup.go).
func TestMachinePoolReconcileReplicasExternallyManaged(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-ext-managed"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	// The MachinePool declares replicas=3 but is managed by an external
	// autoscaler (cluster.x-k8s.io/replicas-managed-by).
	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-cluster-pool-0",
			Namespace:   ns,
			Labels:      map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
			Annotations: map[string]string{clusterv1.ReplicasManagedByAnnotation: "true"},
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(3),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: stringPtr(""),
					},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     3,
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	// The external autoscaler has scaled the cloud node pool to 5 nodes.
	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ListNodePoolsFn = func(_ context.Context, _ string) ([]cceService.NodePoolInfo, error) {
		return []cceService.NodePoolInfo{{NodePoolID: "nodepool-1", Name: "pool-0", DesiredNodeCount: 5, NodeCount: 5, ActiveNodeCount: 5}}, nil
	}
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// The CAPI MachinePool.spec.replicas must be reverse-synced to the
	// cloud-side count (5), not the infra pool spec.
	gotMP := &clusterv1.MachinePool{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mp), gotMP); err != nil {
		t.Fatalf("failed to get MachinePool: %v", err)
	}
	if gotMP.Spec.Replicas == nil || *gotMP.Spec.Replicas != 5 {
		t.Errorf("expected MachinePool.spec.replicas reverse-synced to 5, got %v", gotMP.Spec.Replicas)
	}
	// The provider must not have issued a ScaleNodePool (it is not the
	// source of truth for externally-managed pools).
	if len(fakeSvc.ScaleCalls) != 0 {
		t.Errorf("expected no ScaleNodePool for externally-managed pool, got %v", fakeSvc.ScaleCalls)
	}
}

// TestMachinePoolReconcileNodeRepair verifies node auto-repair: when
// spec.nodeRepair.enabled is set, Abnormal/Error nodes are reset via CCE
// ResetNode (the CCE substitute for EKS NodeRepairConfig).
func TestMachinePoolReconcileNodeRepair(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-node-repair"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Status.ClusterID = "cluster-1"
	cp.Status.Ready = true
	if err := k8sClient.Status().Update(ctx, cp); err != nil {
		t.Fatalf("failed to set control plane status: %v", err)
	}

	mp := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap:   clusterv1.Bootstrap{DataSecretName: stringPtr("")},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrav1beta2.GroupVersion.Group,
						Kind:     "CCEManagedMachinePool",
						Name:     "test-cluster-pool-0",
					},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, mp); err != nil {
		t.Fatalf("failed to create MachinePool: %v", err)
	}
	pool := &infrav1beta2.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "test-cluster"},
		},
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			ClusterName:  "test-cluster",
			NodePoolName: "pool-0",
			Flavor:       "c7.large.2",
			Replicas:     1,
			NodeRepair:   &infrav1beta2.NodeRepairSpec{Enabled: true},
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("failed to create CCEManagedMachinePool: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ListNodesWithStatusFn = func(_ context.Context, _ string) ([]cceService.NodeInfo, error) {
		return []cceService.NodeInfo{
			{UID: "node-abnormal-1", NodePoolID: "nodepool-1", Phase: "Abnormal"},
			{UID: "node-error-1", NodePoolID: "nodepool-1", Phase: "Error"},
			{UID: "node-active-1", NodePoolID: "nodepool-1", Phase: "Active"},
			{UID: "node-other-pool-abnormal", NodePoolID: "nodepool-other", Phase: "Abnormal"},
		}, nil
	}
	r := &CCEManagedMachinePoolReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Only the Abnormal/Error nodes must be reset; Active nodes are left alone.
	if len(fakeSvc.ResetNodeCalls) != 1 {
		t.Fatalf("expected 1 ResetNode call, got %d", len(fakeSvc.ResetNodeCalls))
	}
	reset := fakeSvc.ResetNodeCalls[0]
	if len(reset) != 2 || reset[0] != "node-abnormal-1" || reset[1] != "node-error-1" {
		t.Errorf("expected [node-abnormal-1 node-error-1] to be reset, got %v", reset)
	}
}
