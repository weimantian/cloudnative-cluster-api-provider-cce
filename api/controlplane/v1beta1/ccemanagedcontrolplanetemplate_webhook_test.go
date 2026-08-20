/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestCCEManagedControlPlaneTemplateValidate verifies the template validates
// the wrapped control plane spec while tolerating an empty clusterName (filled
// by the topology controller from a patch).
func TestCCEManagedControlPlaneTemplateValidate(t *testing.T) {
	tmpl := &CCEManagedControlPlaneTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl"},
		Spec: CCEManagedControlPlaneTemplateSpec{Template: CCEManagedControlPlaneTemplateResource{
			Spec: CCEManagedControlPlaneSpec{
				ContainerNetwork: ContainerNetworkSpec{Mode: "eni"},
			},
		}},
	}
	// eni mode requires ENI subnets.
	if err := tmpl.validate(); err == nil {
		t.Error("expected error for eni mode without eniSubnets")
	}
	tmpl.Spec.Template.Spec.ContainerNetwork.ENISubnets = []string{"sub-1"}
	if err := tmpl.validate(); err != nil {
		t.Errorf("expected valid template with empty clusterName, got %v", err)
	}
}

// TestCCEManagedControlPlaneTemplateDefault verifies the template applies the
// same spec-level defaults as the control plane object.
func TestCCEManagedControlPlaneTemplateDefault(t *testing.T) {
	tmpl := &CCEManagedControlPlaneTemplate{
		Spec: CCEManagedControlPlaneTemplateSpec{Template: CCEManagedControlPlaneTemplateResource{
			Spec: CCEManagedControlPlaneSpec{},
		}},
	}
	if err := tmpl.Default(nil, tmpl); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if tmpl.Spec.Template.Spec.Category != "Turbo" ||
		tmpl.Spec.Template.Spec.ContainerNetwork.Mode != "eni" ||
		tmpl.Spec.Template.Spec.Flavor != "cce.s1.small" {
		t.Errorf("expected defaults applied, got %+v", tmpl.Spec.Template.Spec)
	}
}
