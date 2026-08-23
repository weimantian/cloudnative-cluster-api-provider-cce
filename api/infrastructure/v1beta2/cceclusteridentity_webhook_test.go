/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
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
