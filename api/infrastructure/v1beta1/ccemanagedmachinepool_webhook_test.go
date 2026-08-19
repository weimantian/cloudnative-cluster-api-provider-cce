/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"context"
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

func TestMachinePoolUpdateConfigDefaults(t *testing.T) {
	m := validPool()
	if err := m.Default(context.Background(), m); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if m.Spec.UpdateConfig.MaxUnavailable != 1 {
		t.Errorf("expected MaxUnavailable defaulted to 1, got %d", m.Spec.UpdateConfig.MaxUnavailable)
	}

	// An explicit value must be preserved.
	explicit := validPool()
	explicit.Spec.UpdateConfig.MaxUnavailable = 3
	if err := explicit.Default(context.Background(), explicit); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if explicit.Spec.UpdateConfig.MaxUnavailable != 3 {
		t.Errorf("expected explicit MaxUnavailable=3 preserved, got %d", explicit.Spec.UpdateConfig.MaxUnavailable)
	}
}

func TestMachinePoolUpdateConfigValidation(t *testing.T) {
	for _, mu := range []int32{-1, 21, 100} {
		m := validPool()
		m.Spec.UpdateConfig.MaxUnavailable = mu
		if err := m.validate(); err == nil {
			t.Errorf("expected MaxUnavailable=%d to be rejected", mu)
		}
	}
	// 0 is the "unset" sentinel: defaulted to 1, not rejected.
	unset := validPool()
	if err := unset.validate(); err != nil {
		t.Errorf("expected MaxUnavailable=0 (unset) to pass, got %v", err)
	}
	ok := validPool()
	ok.Spec.UpdateConfig.MaxUnavailable = 20
	if err := ok.validate(); err != nil {
		t.Errorf("expected MaxUnavailable=20 to pass, got %v", err)
	}
}
