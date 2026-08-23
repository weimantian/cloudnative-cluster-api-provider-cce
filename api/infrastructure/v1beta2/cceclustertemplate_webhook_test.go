/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

// TestCCEClusterTemplateValidate verifies the template delegates validation
// to the wrapped CCECluster spec (fail fast at ClusterClass apply time).
func TestCCEClusterTemplateValidate(t *testing.T) {
	tmpl := &CCEClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl"},
		Spec: CCEClusterTemplateSpec{Template: CCEClusterTemplateResource{
			Spec: CCEClusterSpec{},
		}},
	}
	if err := tmpl.validate(); err == nil {
		t.Error("expected error for empty region")
	}
	tmpl.Spec.Template.Spec.Region = "cn-north-4"
	if err := tmpl.validate(); err != nil {
		t.Errorf("expected valid template, got %v", err)
	}
}

// TestCCEManagedMachinePoolTemplateValidate verifies the template validates
// the wrapped node pool spec while tolerating an empty clusterName (filled by
// the topology controller from a patch).
func TestCCEManagedMachinePoolTemplateValidate(t *testing.T) {
	tmpl := &CCEManagedMachinePoolTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl"},
		Spec: CCEManagedMachinePoolTemplateSpec{Template: CCEManagedMachinePoolTemplateResource{
			Spec: CCEManagedMachinePoolSpec{},
		}},
	}
	// Empty clusterName must be tolerated; missing flavor/AZ/OS/rootVolume must not.
	if err := tmpl.validate(); err == nil {
		t.Error("expected error for missing required node pool fields")
	}
	tmpl.Spec.Template.Spec.Flavor = "c7.large.2"
	tmpl.Spec.Template.Spec.AvailabilityZone = "cn-north-4a"
	tmpl.Spec.Template.Spec.OS = "Huawei Cloud EulerOS 2.0"
	tmpl.Spec.Template.Spec.RootVolume = &common.NodeVolume{Size: 100, Type: "SSD"}
	if err := tmpl.validate(); err != nil {
		t.Errorf("expected valid template with empty clusterName, got %v", err)
	}
}
