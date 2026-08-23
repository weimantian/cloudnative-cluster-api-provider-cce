/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/network"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func TestCCEClusterReconcileReady(t *testing.T) {
	ctx := context.Background()
	ns := "ccecluster-test-ready"
	createNamespace(t, ns)

	cluster, _, _ := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")

	fakeNet := fakes.NewFakeNetworkValidator()
	r := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakeNet, nil
		},
	}

	// With credentials resolved, validation runs and the shell becomes ready.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	got := &infrav1beta2.CCECluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got); err != nil {
		t.Fatalf("failed to get CCECluster: %v", err)
	}
	if !got.Status.Ready {
		t.Error("expected CCECluster Status.Ready = true")
	}
	if c := capiconditions.Get(got, conditions.NetworkReadyCondition); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected NetworkReady=True, got %v", c)
	}
	if !hasFinalizer(got.Finalizers, CCEClusterFinalizer) {
		t.Error("expected CCEClusterFinalizer to be set")
	}
}

func TestCCEClusterReconcileNetworkFailure(t *testing.T) {
	ctx := context.Background()
	ns := "ccecluster-test-netfail"
	createNamespace(t, ns)

	cluster, _, _ := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")

	fakeNet := fakes.NewFakeNetworkValidator()
	fakeNet.Issues = []network.Issue{
		{Field: "serviceNetwork.cidr", Message: "service CIDR must not overlap the VPC CIDR"},
	}
	r := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakeNet, nil
		},
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected a requeue after network validation failure")
	}

	got := &infrav1beta2.CCECluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got); err != nil {
		t.Fatalf("failed to get CCECluster: %v", err)
	}
	if got.Status.Ready {
		t.Error("expected CCECluster not ready after network validation failure")
	}
	if c := capiconditions.Get(got, conditions.NetworkReadyCondition); c == nil || c.Status != metav1.ConditionFalse {
		t.Errorf("expected NetworkReady=False, got %v", c)
	}
}

func TestCCEClusterReconcileManagedNetwork(t *testing.T) {
	ctx := context.Background()
	ns := "ccecluster-test-managed"
	createNamespace(t, ns)

	cluster, cceCluster, _ := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")

	// Managed mode: no vpc.id, a cidr instead, plus an enabled NAT gateway.
	cceCluster.Spec.Network = common.NetworkSpec{
		VPC:        common.VPC{CIDR: "10.10.0.0/16"},
		Subnets:    []common.Subnet{{Name: "nodes", CIDR: "10.10.0.0/24", Type: common.SubnetTypeNode}},
		NatGateway: &common.NatGatewaySpec{},
	}
	if err := k8sClient.Update(ctx, cceCluster); err != nil {
		t.Fatalf("failed to set managed network spec: %v", err)
	}

	fakeMgr := &fakes.FakeNetworkManager{}
	fakeNet := fakes.NewFakeNetworkValidator()
	r := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakeNet, nil
		},
		NetworkServiceFactory: func(_, _, _ string) (network.ManagerInterface, error) {
			return fakeMgr, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if fakeMgr.ReconcileCalls != 3 {
		t.Errorf("expected 3 step calls (Vpc+Subnets+NatGateway), got %d", fakeMgr.ReconcileCalls)
	}

	got := &infrav1beta2.CCECluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got); err != nil {
		t.Fatalf("failed to get CCECluster: %v", err)
	}
	if !got.Status.Ready {
		t.Error("expected CCECluster Status.Ready = true after managed reconcile")
	}
	if got.Spec.Network.VPC.ResourceID == "" {
		t.Error("expected spec.network.vpc.resourceID to be backfilled and persisted")
	}
	if len(got.Spec.Network.Subnets) != 1 || got.Spec.Network.Subnets[0].ResourceID == "" {
		t.Error("expected subnet resourceID to be backfilled and persisted")
	}
	// Each managed step carries a dedicated condition (mirrors CAPA).
	for _, cType := range []string{conditions.VpcReadyCondition, conditions.SubnetsReadyCondition, conditions.NatGatewaysReadyCondition} {
		if c := capiconditions.Get(got, cType); c == nil || c.Status != metav1.ConditionTrue {
			t.Errorf("expected condition %s=True, got %v", cType, c)
		}
	}
}

// TestCCEClusterReconcileAdoptedNetwork verifies the adopt state: vpc.id set
// WITH the owned tag is managed (subnets/NAT reconciled), unlike BYO.
func TestCCEClusterReconcileAdoptedNetwork(t *testing.T) {
	ctx := context.Background()
	ns := "ccecluster-test-adopted"
	createNamespace(t, ns)

	cluster, cceCluster, _ := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")

	// Adopted: an existing VPC carrying the provider owned tag.
	cceCluster.Spec.Network = common.NetworkSpec{
		VPC:        common.VPC{ID: "vpc-existing", Tags: common.Tags{"cluster-api-provider-cce.cluster.test-cluster": "owned"}},
		NatGateway: &common.NatGatewaySpec{},
	}
	if err := k8sClient.Update(ctx, cceCluster); err != nil {
		t.Fatalf("failed to set adopted network spec: %v", err)
	}

	fakeMgr := &fakes.FakeNetworkManager{}
	r := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakes.NewFakeNetworkValidator(), nil
		},
		NetworkServiceFactory: func(_, _, _ string) (network.ManagerInterface, error) {
			return fakeMgr, nil
		},
	}

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Adopted VPC is managed: the three steps run (not skipped as BYO).
	if fakeMgr.ReconcileCalls != 3 {
		t.Errorf("expected 3 step calls for adopted network, got %d", fakeMgr.ReconcileCalls)
	}
	got := &infrav1beta2.CCECluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got); err != nil {
		t.Fatalf("failed to get CCECluster: %v", err)
	}
	if !got.Status.Ready {
		t.Error("expected adopted CCECluster Status.Ready = true")
	}
}

func TestCCEClusterReconcileManagedNetworkFailure(t *testing.T) {
	ctx := context.Background()
	ns := "ccecluster-test-managed-fail"
	createNamespace(t, ns)

	cluster, cceCluster, _ := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")

	cceCluster.Spec.Network = common.NetworkSpec{
		VPC: common.VPC{CIDR: "10.10.0.0/16"},
	}
	if err := k8sClient.Update(ctx, cceCluster); err != nil {
		t.Fatalf("failed to set managed network spec: %v", err)
	}

	fakeMgr := &fakes.FakeNetworkManager{
		ReconcileVpcFn: func(_ context.Context, _ *common.NetworkSpec, _ string) error {
			return errors.New("CreateVpc quota exceeded")
		},
	}
	r := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakes.NewFakeNetworkValidator(), nil
		},
		NetworkServiceFactory: func(_, _, _ string) (network.ManagerInterface, error) {
			return fakeMgr, nil
		},
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected requeue after managed network failure")
	}
	got := &infrav1beta2.CCECluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got); err != nil {
		t.Fatalf("failed to get CCECluster: %v", err)
	}
	if got.Status.Ready {
		t.Error("expected not ready after managed network failure")
	}
}

func TestCCEClusterDeleteManagedNetwork(t *testing.T) {
	ctx := context.Background()
	ns := "ccecluster-test-managed-delete"
	createNamespace(t, ns)

	cluster, cceCluster, _ := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")

	// Managed network with recorded resource IDs (post-creation state);
	// the control plane object must be deleted first (the reconciler waits
	// for it before tearing down the network).
	cceCluster.Spec.Network = common.NetworkSpec{
		VPC:        common.VPC{ResourceID: "vpc-managed-fake"},
		Subnets:    []common.Subnet{{ResourceID: "subnet-managed-fake-0"}},
		NatGateway: &common.NatGatewaySpec{ResourceID: "nat-fake", EIPResourceID: "eip-fake"},
	}
	if err := k8sClient.Update(ctx, cceCluster); err != nil {
		t.Fatalf("failed to set managed network spec: %v", err)
	}

	// First reconcile adds the finalizer (and reconciles the managed
	// network through the fake manager) so a subsequent Delete keeps the
	// object around with a deletion timestamp.
	setupR := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakes.NewFakeNetworkValidator(), nil
		},
		NetworkServiceFactory: func(_, _, _ string) (network.ManagerInterface, error) {
			return &fakes.FakeNetworkManager{}, nil
		},
	}
	if _, err := setupR.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("setup Reconcile returned error: %v", err)
	}
	cp := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "test-cluster-control-plane"}, cp); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	if err := k8sClient.Delete(ctx, cp); err != nil {
		t.Fatalf("failed to delete control plane: %v", err)
	}

	// envtest removes the object asynchronously; wait until it is gone so
	// the reconciler's "control plane still exists" gate is open.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "test-cluster-control-plane"}, &controlplanev1beta2.CCEManagedControlPlane{}); apierrors.IsNotFound(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	fakeMgr := &fakes.FakeNetworkManager{}
	r := &CCEClusterReconciler{
		Client: k8sClient,
		NetworkValidatorFactory: func(_, _, _ string) (network.ValidatorInterface, error) {
			return fakes.NewFakeNetworkValidator(), nil
		},
		NetworkServiceFactory: func(_, _, _ string) (network.ManagerInterface, error) {
			return fakeMgr, nil
		},
	}

	// Mark the shell deleting (finalizer keeps it around).
	if err := k8sClient.Delete(ctx, cceCluster); err != nil {
		t.Fatalf("failed to delete CCECluster: %v", err)
	}

	res, rerr := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if rerr != nil {
		t.Fatalf("Reconcile returned error: %v", rerr)
	}
	_ = res
	if fakeMgr.DeleteCalls != 1 {
		t.Errorf("expected 1 DeleteNetwork call, got %d", fakeMgr.DeleteCalls)
	}
	got := &infrav1beta2.CCECluster{}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got)
	if err == nil {
		if hasFinalizer(got.Finalizers, CCEClusterFinalizer) {
			t.Error("expected finalizer removed after managed network deletion")
		}
	} else if !apierrors.IsNotFound(err) {
		t.Fatalf("failed to get CCECluster after deletion: %v", err)
	}
}
