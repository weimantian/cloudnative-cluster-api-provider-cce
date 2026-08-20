/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
)

func deleteDefaultControllerIdentity(t *testing.T) {
	t.Helper()
	id := &infrav1beta1.CCEClusterControllerIdentity{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: infrav1beta1.CCEClusterControllerIdentityName}, id); err != nil {
		return
	}
	_ = k8sClient.Delete(context.Background(), id)
}

// TestControllerIdentityReconcilerCreatesDefault verifies the reconciler
// creates the "default" CCEClusterControllerIdentity singleton for a control
// plane that uses (or defaults to) the controller identity.
func TestControllerIdentityReconcilerCreatesDefault(t *testing.T) {
	ctx := context.Background()
	ns := "id-test-create"
	createNamespace(t, ns)
	deleteDefaultControllerIdentity(t)

	_, _, cp := newTestCluster(t, ns) // no identityRef -> default controller identity

	r := &CCEClusterControllerIdentityReconciler{Client: k8sClient}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	got := &infrav1beta1.CCEClusterControllerIdentity{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: infrav1beta1.CCEClusterControllerIdentityName}, got); err != nil {
		t.Fatalf("expected default identity to be created: %v", err)
	}
	if got.Spec.AllowedNamespaces != nil {
		t.Errorf("expected nil AllowedNamespaces (any namespace), got %+v", got.Spec.AllowedNamespaces)
	}
}

// TestControllerIdentityReconcilerSkipsStaticIdentity verifies no singleton is
// created for a control plane using a static identity (it carries its own
// credentials).
func TestControllerIdentityReconcilerSkipsStaticIdentity(t *testing.T) {
	ctx := context.Background()
	ns := "id-test-skip"
	createNamespace(t, ns)
	deleteDefaultControllerIdentity(t)

	createStaticIdentity(t, "skip-static")
	_, _, cp := newTestCluster(t, ns)
	cp.Spec.IdentityRef = &corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "skip-static"}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set identityRef: %v", err)
	}

	r := &CCEClusterControllerIdentityReconciler{Client: k8sClient}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	got := &infrav1beta1.CCEClusterControllerIdentity{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: infrav1beta1.CCEClusterControllerIdentityName}, got)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected no default identity for static identity cluster, err=%v", err)
	}
}

// TestControllerIdentityReconcilerIdempotent verifies repeated reconciles do
// not error or create duplicates.
func TestControllerIdentityReconcilerIdempotent(t *testing.T) {
	ctx := context.Background()
	ns := "id-test-idem"
	createNamespace(t, ns)
	deleteDefaultControllerIdentity(t)

	_, _, cp := newTestCluster(t, ns)
	r := &CCEClusterControllerIdentityReconciler{Client: k8sClient}

	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
			t.Fatalf("reconcile %d returned error: %v", i, err)
		}
	}
	list := &infrav1beta1.CCEClusterControllerIdentityList{}
	if err := k8sClient.List(ctx, list); err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected exactly 1 controller identity, got %d", len(list.Items))
	}
}
