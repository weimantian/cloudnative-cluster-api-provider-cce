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

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
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
	want := controlplanev1beta2.AccessPolicySpec{
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
	cp.Spec.AccessPolicies = []controlplanev1beta2.AccessPolicySpec{
		{Name: "view-all", PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIds: []string{"user-1"}},
		{Name: "ops-default-ns", PolicyType: "CCEAdminPolicy", PrincipalType: "group", PrincipalIds: []string{"grp-1"}, Namespaces: []string{"default"}},
	}
	if err := k8sClient.Update(ctx, cp); err != nil {
		t.Fatalf("failed to set access policies: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
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
	// spec, and "stale-policy" was never created by this provider -> 1 update +
	// 1 delete (stale-policy belongs to another cluster and must survive).
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-view-all", Name: "view-all", ClusterIDs: []string{"cluster-1"}, PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-1"}, Namespaces: []string{"*"}},
		{PolicyID: "pol-ops", Name: "ops-default-ns", ClusterIDs: []string{"cluster-1"}, PolicyType: "CCEAdminPolicy", PrincipalType: "group", PrincipalIDs: []string{"grp-1"}, Namespaces: []string{"default"}},
		{PolicyID: "pol-stale", Name: "stale-policy", ClusterIDs: []string{"cluster-other"}, PolicyType: "CCEViewPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-9"}, Namespaces: []string{"*"}},
	}
	// Drift the "view-all" policyType.
	latest := &controlplanev1beta2.CCEManagedControlPlane{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cp), latest); err != nil {
		t.Fatalf("failed to get control plane: %v", err)
	}
	latest.Spec.AccessPolicies = []controlplanev1beta2.AccessPolicySpec{
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
	// Only the policy this provider created ("ops-default-ns") is deleted; the
	// stale policy was never ours (B4: access policies are account-scoped) and
	// must survive.
	if len(fakeSvc.AccessPolicyDelete) != 1 || fakeSvc.AccessPolicyDelete[0] != "pol-ops" {
		t.Errorf("expected only pol-ops deleted, got %v", fakeSvc.AccessPolicyDelete)
	}
}

// TestControlPlaneReconcileAccessPoliciesClusterScope covers the B4 cluster-scope
// guard: a cloud policy is managed only when its scope includes this cluster's
// ID (or the "*" wildcard) AND its name is in status.accessPolicies. A
// same-named policy scoped to another cluster is never adopted, updated or
// deleted.
func TestControlPlaneReconcileAccessPoliciesClusterScope(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-accesspolicy-clusterscope"
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
	r := &CCEManagedControlPlaneReconciler{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}

	// First reconcile creates our policy (scoped to cluster-1) and latches its
	// name in status.accessPolicies.
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(fakeSvc.AccessPolicyCreate) != 1 || fakeSvc.AccessPolicyCreate[0].ClusterID != "cluster-1" {
		t.Fatalf("expected our policy created in cluster-1, got %+v", fakeSvc.AccessPolicyCreate)
	}

	// (a) The account list now holds OUR drifted policy (scoped to cluster-1)
	// and a FOREIGN same-named policy scoped to another cluster. Only ours may
	// be updated; the foreign one must never be touched.
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-foreign", Name: "shared", ClusterIDs: []string{"cluster-other"}, PolicyType: "CCEAdminPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-9"}, Namespaces: []string{"*"}},
		{PolicyID: "access-policy-shared", Name: "shared", ClusterIDs: []string{"cluster-1"}, PolicyType: "CCEAdminPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-1"}, Namespaces: []string{"*"}},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile (collision) returned error: %v", err)
	}
	if len(fakeSvc.AccessPolicyUpdate) != 1 {
		t.Fatalf("expected exactly 1 updated policy, got %d", len(fakeSvc.AccessPolicyUpdate))
	}
	if len(fakeSvc.AccessPolicyUpdateID) != 1 || fakeSvc.AccessPolicyUpdateID[0] != "access-policy-shared" {
		t.Errorf("expected only our policy updated, got %v", fakeSvc.AccessPolicyUpdateID)
	}
	if len(fakeSvc.AccessPolicyDelete) != 0 {
		t.Errorf("the foreign policy must never be deleted, got %v", fakeSvc.AccessPolicyDelete)
	}

	// (c) Empty the spec, leaving only the FOREIGN same-named policy in the
	// account. Its name is still latched in status.accessPolicies, but the
	// cluster scope excludes it, so it must survive.
	fakeSvc.AccessPolicies = []cceService.AccessPolicyInfo{
		{PolicyID: "pol-foreign", Name: "shared", ClusterIDs: []string{"cluster-other"}, PolicyType: "CCEAdminPolicy", PrincipalType: "user", PrincipalIDs: []string{"user-9"}, Namespaces: []string{"*"}},
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
	if len(fakeSvc.AccessPolicyDelete) != 0 {
		t.Errorf("a policy scoped to another cluster must never be deleted, got %v", fakeSvc.AccessPolicyDelete)
	}
}
