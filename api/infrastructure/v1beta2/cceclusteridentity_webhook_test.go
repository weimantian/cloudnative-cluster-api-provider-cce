/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterStaticIdentityValidate(t *testing.T) {
	id := &CCEClusterStaticIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "static"},
	}
	if err := id.validate(); err == nil {
		t.Error("expected error for empty secretRef")
	}
	id.Spec.SecretRef = "my-secret"
	if err := id.validate(); err != nil {
		t.Errorf("expected valid identity, got %v", err)
	}
}

// TestClusterStaticIdentityValidateUpdateSecretRefImmutable verifies that
// updating Spec.SecretRef on an existing identity is rejected. Swapping
// credentials behind the controller's back would silently break live reconciles
// that hold the old Secret in their cache. Mirrors CAPA's
// AWSClusterStaticIdentity webhook.
func TestClusterStaticIdentityValidateUpdateSecretRefImmutable(t *testing.T) {
	ctx := context.Background()
	old := &CCEClusterStaticIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "static"},
		Spec:       CCEClusterStaticIdentitySpec{SecretRef: "original-secret"},
	}
	new := &CCEClusterStaticIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "static"},
		Spec:       CCEClusterStaticIdentitySpec{SecretRef: "new-secret"},
	}
	if _, err := (&CCEClusterStaticIdentity{}).ValidateUpdate(ctx, old, new); err == nil {
		t.Error("expected error when changing secretRef")
	}
	// Same secretRef: no error.
	if _, err := (&CCEClusterStaticIdentity{}).ValidateUpdate(ctx, old, old); err != nil {
		t.Errorf("expected nil error when secretRef unchanged, got %v", err)
	}
}

func TestClusterRoleIdentityValidate(t *testing.T) {
	id := &CCEClusterRoleIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "role"},
	}
	if err := id.validate(); err == nil {
		t.Error("expected error for empty agencyName")
	}
	id.Spec.AgencyName = "my-agency"
	if err := id.validate(); err != nil {
		t.Errorf("expected valid identity, got %v", err)
	}
}

func TestClusterControllerIdentityValidate(t *testing.T) {
	// Empty AllowedNamespaces (nil) passes.
	ok := &CCEClusterControllerIdentity{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	if err := ok.validate(); err != nil {
		t.Errorf("expected nil allowedNamespaces to pass, got %v", err)
	}
	// A malformed selector is rejected.
	bad := &CCEClusterControllerIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: CCEClusterControllerIdentitySpec{
			AllowedNamespaces: &AllowedNamespaces{
				Selector: metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      "env",
						Operator: "BogusOperator",
					}},
				},
			},
		},
	}
	if err := bad.validate(); err == nil {
		t.Error("expected error for malformed namespace selector")
	}
}
// TestClusterControllerIdentitySingletonName verifies that
// CCEClusterControllerIdentity can only be created with name "default".
func TestClusterControllerIdentitySingletonName(t *testing.T) {
	ctx := context.Background()
	bad := &CCEClusterControllerIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "not-default"},
	}
	if _, err := (&CCEClusterControllerIdentity{}).ValidateCreate(ctx, bad); err == nil {
		t.Error("expected error for non-singleton name")
	}
	good := &CCEClusterControllerIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
	}
	if _, err := (&CCEClusterControllerIdentity{}).ValidateCreate(ctx, good); err != nil {
		t.Errorf("expected nil error for name=default, got %v", err)
	}
}

// TestClusterControllerIdentityImmutability verifies that name and spec
// cannot change on update.
func TestClusterControllerIdentityImmutability(t *testing.T) {
	ctx := context.Background()
	old := &CCEClusterControllerIdentity{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	newName := &CCEClusterControllerIdentity{ObjectMeta: metav1.ObjectMeta{Name: "renamed"}}
	if _, err := (&CCEClusterControllerIdentity{}).ValidateUpdate(ctx, old, newName); err == nil {
		t.Error("expected error when renaming controller identity")
	}
	newSpec := &CCEClusterControllerIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: CCEClusterControllerIdentitySpec{
			AllowedNamespaces: &AllowedNamespaces{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			},
		},
	}
	if _, err := (&CCEClusterControllerIdentity{}).ValidateUpdate(ctx, old, newSpec); err == nil {
		t.Error("expected error when changing spec")
	}
	// Same name + same spec: pass.
	if _, err := (&CCEClusterControllerIdentity{}).ValidateUpdate(ctx, old, old); err != nil {
		t.Errorf("expected nil error for unchanged identity, got %v", err)
	}
}

// TestClusterRoleIdentityValidateUpdateAgencyNameImmutable verifies that
// agencyName cannot change on update (swapping the trust principal
// mid-flight would invalidate every cached token).
func TestClusterRoleIdentityValidateUpdateAgencyNameImmutable(t *testing.T) {
	ctx := context.Background()
	old := &CCEClusterRoleIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "role"},
		Spec:       CCEClusterRoleIdentitySpec{AgencyName: "original-agency"},
	}
	new := &CCEClusterRoleIdentity{
		ObjectMeta: metav1.ObjectMeta{Name: "role"},
		Spec:       CCEClusterRoleIdentitySpec{AgencyName: "new-agency"},
	}
	if _, err := (&CCEClusterRoleIdentity{}).ValidateUpdate(ctx, old, new); err == nil {
		t.Error("expected error when changing agencyName")
	}
	if _, err := (&CCEClusterRoleIdentity{}).ValidateUpdate(ctx, old, old); err != nil {
		t.Errorf("expected nil error for unchanged role identity, got %v", err)
	}
}
