/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"testing"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

// validPool returns a pool that passes the required-field validation so tests
// can focus on the field under test.
func validPool() *CCEManagedMachinePool {
	return &CCEManagedMachinePool{Spec: CCEManagedMachinePoolSpec{
		ClusterName:      "test-cluster",
		Flavor:           "c6.large.2",
		AvailabilityZone: "cn-north-4a",
		OS:               "Huawei Cloud EulerOS 2.0",
		RootVolume:       &common.NodeVolume{Size: 40, Type: "SSD"},
	}}
}

func TestMachinePoolFlavorValidation(t *testing.T) {
	cases := []struct {
		name   string
		flavor string
		wantOK bool
	}{
		{name: "standard family", flavor: "c6.large.2", wantOK: true},
		{name: "xlarge", flavor: "c7.xlarge.4", wantOK: true},
		{name: "eni variant", flavor: "c6sne.large.2", wantOK: true},
		{name: "generation prefix", flavor: "s6.medium.1", wantOK: true},
		{name: "empty", flavor: "", wantOK: false},
		{name: "bare family", flavor: "c6", wantOK: false},
		{name: "uppercase", flavor: "C6.large.2", wantOK: false},
		{name: "spaces", flavor: "c6 large 2", wantOK: false},
		{name: "trailing dot", flavor: "c6.large.2.", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validPool()
			m.Spec.Flavor = tc.flavor
			err := m.validate()
			if (err == nil) != tc.wantOK {
				t.Errorf("validate() with flavor %q: got err=%v, wantOK=%v", tc.flavor, err, tc.wantOK)
			}
		})
	}
}

func TestMachinePoolFlavorAllowlist(t *testing.T) {
	old := ValidFlavors
	ValidFlavors = []string{"c6.large.2", "c7.large.2"}
	defer func() { ValidFlavors = old }()

	ok := validPool()
	ok.Spec.Flavor = "c6.large.2"
	if err := ok.validate(); err != nil {
		t.Errorf("expected allowlisted flavor to pass, got %v", err)
	}

	rejected := validPool()
	rejected.Spec.Flavor = "c6.xlarge.4"
	if err := rejected.validate(); err == nil {
		t.Error("expected flavor outside allowlist to be rejected")
	}
}
