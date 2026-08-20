/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

// TestAccessPolicyDrifted verifies the drift detection (empty spec namespaces
// default to ["*"]).
func TestAccessPolicyDrifted(t *testing.T) {
	got := cceService.AccessPolicyInfo{
		PolicyType: "CCEViewPolicy", PrincipalType: "user",
		PrincipalIDs: []string{"user-1"}, Namespaces: []string{"*"},
	}
	want := controlplanev1beta1.AccessPolicySpec{
		Name: "p", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIds: []string{"user-1"},
	}
	if accessPolicyDrifted(got, want) {
		t.Error("expected no drift (empty namespaces == [\"*\"])")
	}
	want.PolicyType = "CCEAdminPolicy"
	if !accessPolicyDrifted(got, want) {
		t.Error("expected policyType drift")
	}
	want.PolicyType = "CCEViewPolicy"
	want.PrincipalIds = []string{"user-2"}
	if !accessPolicyDrifted(got, want) {
		t.Error("expected principal drift")
	}
	want.PrincipalIds = []string{"user-1"}
	want.Namespaces = []string{"default"}
	if !accessPolicyDrifted(got, want) {
		t.Error("expected namespace drift")
	}
}

// TestControlPlaneReconcileAccessPolicies verifies the access-policy reconcile:
// create missing, update drift, delete removed.
func TestControlPlaneReconcileAccessPolicies(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-accesspolicy"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)
	cp.Spec.AccessPolicies = []controlplanev1beta1.AccessPolicySpec{
		{Name: "view-all", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIds: []string{"user-1"}},
		{Name: "ops-default-ns", PolicyType: "CCEAdminPolicy", PrincipalType: "group", PrincipalIds: []string{"grp-1"}, Namespaces: []string{"default"}},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set access policies: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	// First reconcile: both policies missing -> create.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.AccessPolicyCreate) != 2 {
		t.Fatalf("expected 2 created policies, got %d (%+v)", len(fakeSvc.AccessPolicyCreate), fakeSvc.AccessPolicyCreate)
	}

	// Second reconcile: the cloud now has both created policies plus a stale
	// one. "view-all" drifts (policyType), "ops-default-ns" is removed from
	// spec, and "stale-policy" was never ours -> 1 update + 2 deletes.
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-view-all", Name: "view-all", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-1"}, Namespaces: []string{"*"}},
		{PolicyID: "pol-ops", Name: "ops-default-ns", PolicyType: "CCEAdminPolicy", PrincipalType: "group", PrincipalIDs: []string{"grp-1"}, Namespaces: []string{"default"}},
		{PolicyID: "pol-stale", Name: "stale-policy", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-9"}, Namespaces: []string{"*"}},
	}
	// Drift the "view-all" policyType.
	latest := &controlplanev1beta1.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	latest.Spec.AccessPolicies = []controlplanev1beta1.AccessPolicySpec{
		{Name: "view-all", PolicyType: "CCEClusterAdminPolicy", PrincipalType: "user", PrincipalIds: []string{"user-1"}},
	}
	if err := k8sClient.Update(ctx, latest); err != nil {
		t.Fatalf("failed to update access policies: %v", err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile (drift) returned error: %v", err)
	}
	if len(fakeSvc.AccessPolicyUpdate) != 1 {
		t.Errorf("expected 1 updated policy, got %d", len(fakeSvc.AccessPolicyUpdate))
	}
	if len(fakeSvc.AccessPolicyDelete) != 2 {
		t.Errorf("expected 2 deleted policies (ops-default-ns + stale-policy), got %v", fakeSvc.AccessPolicyDelete)
	}
}
