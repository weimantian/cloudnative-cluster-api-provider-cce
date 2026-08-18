/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

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
						APIGroup: infrav1beta1.GroupVersion.Group,
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
	pool := &infrav1beta1.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: infrav1beta1.CCEManagedMachinePoolSpec{
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

	got := &infrav1beta1.CCEManagedMachinePool{}
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
	// not a delta (questionnaire Q3 absolute semantics).
	if len(fakeSvc.CreatedNodePools) != 1 {
		t.Fatalf("expected 1 created node pool, got %d", len(fakeSvc.CreatedNodePools))
	}
	if fakeSvc.CreatedNodePools[0].ClusterID != "cluster-1" || fakeSvc.CreatedNodePools[0].InitialNodeCount != 3 {
		t.Errorf("unexpected create node pool input: %+v", fakeSvc.CreatedNodePools[0])
	}
	if len(fakeSvc.ScaleCalls) != 1 || fakeSvc.ScaleCalls[0] != 3 {
		t.Errorf("expected ScaleNodePool with absolute target 3, got %v", fakeSvc.ScaleCalls)
	}
}

func TestMachinePoolReconcileWaitsForControlPlane(t *testing.T) {
	ctx := context.Background()
	ns := "mp-test-waiting"
	createNamespace(t, ns)

	cluster, _, _ := newTestCluster(t, ns)
	markInfrastructureProvisioned(t, cluster)
	// Control plane NOT ready.

	pool := &infrav1beta1.CCEManagedMachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-pool-0",
			Namespace: ns,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: infrav1beta1.CCEManagedMachinePoolSpec{
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
