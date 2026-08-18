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
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/conditions"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

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
	if got.Status.ControlPlaneEndpoint == nil || got.Status.ControlPlaneEndpoint.Host != "https://10.0.0.10" || got.Status.ControlPlaneEndpoint.Port != 5443 {
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
