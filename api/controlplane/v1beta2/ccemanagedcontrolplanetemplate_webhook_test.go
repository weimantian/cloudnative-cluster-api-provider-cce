/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

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
				EndpointAccess:   EndpointAccessSpec{Private: true},
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

// TestCCEManagedControlPlaneTemplateValidateCIDRAndSemver covers the two new
// validations added in P1-#5: ContainerNetwork.CIDR format and Version
// semver format. k8s ParseSemantic only accepts full vMAJOR.MINOR.PATCH forms
// (and their pre-release variants), so all test cases use that format.
func TestCCEManagedControlPlaneTemplateValidateCIDRAndSemver(t *testing.T) {
	cases := []struct {
		name  string
		cidr  string
		ver   string
		valid bool
	}{
		{"validIPv4AndSemver", "10.0.0.0/16", "v1.30.1", true},
		{"validIPv6AndPrerelease", "2001:db8::/64", "v1.30.1-rc.1", true},
		{"emptyCIDRUsesDefault", "", "v1.30.0", true},
		{"invalidCIDROutOfRange", "10.0.0.999/16", "v1.30.0", false},
		{"invalidCIDRNoSlash", "10.0.0.0", "v1.30.0", false},
		{"invalidSemverNoPatch", "10.0.0.0/16", "v1.30", false},
		{"invalidSemverSuffix", "10.0.0.0/16", "v1.30.0!", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := &CCEManagedControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "cp1", Namespace: "default"},
				Spec: CCEManagedControlPlaneSpec{
					ClusterName: "test",
					Category:    "Turbo",
					ContainerNetwork: ContainerNetworkSpec{
						Mode: "eni",
						CIDR: tc.cidr,
					},
					Version: tc.ver,
					EndpointAccess: EndpointAccessSpec{Private: true},
				},
			}
			cp.Spec.ContainerNetwork.ENISubnets = []string{"subnet-1"}
			err := cp.validate()
			if tc.valid && err != nil {
				t.Errorf("expected valid, got err: %v", err)
			}
			if !tc.valid && err == nil {
				t.Errorf("expected invalid, got nil")
			}
		})
	}
}
