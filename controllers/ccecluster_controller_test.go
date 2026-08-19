/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/network"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
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

	got := &infrav1beta1.CCECluster{}
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

	got := &infrav1beta1.CCECluster{}
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
