/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"
	"testing"
)

func TestCCEClusterDefaultIsNoOp(t *testing.T) {
	c := &CCECluster{Spec: CCEClusterSpec{Region: "cn-north-4"}}
	if err := c.Default(context.Background(), c); err != nil {
		t.Fatalf("CCECluster.Default() error = %v, want nil", err)
	}
}

func TestCCEClusterValidateDeleteAllows(t *testing.T) {
	warnings, err := (&CCECluster{}).ValidateDelete(context.Background(), &CCECluster{})
	if err != nil || len(warnings) != 0 {
		t.Errorf("CCECluster.ValidateDelete() = (%v, %v), want (nil, nil)", warnings, err)
	}
}

func TestCCEManagedMachinePoolTemplateDefaultAppliesDefaults(t *testing.T) {
	tpl := &CCEManagedMachinePoolTemplate{
		Spec: CCEManagedMachinePoolTemplateSpec{
			Template: CCEManagedMachinePoolTemplateResource{
				Spec: CCEManagedMachinePoolSpec{},
			},
		},
	}
	if err := tpl.Default(context.Background(), tpl); err != nil {
		t.Fatalf("CCEManagedMachinePoolTemplate.Default() error = %v, want nil", err)
	}
	if got := tpl.Spec.Template.Spec.UpdateConfig.MaxUnavailable; got != defaultMaxUnavailable {
		t.Errorf("MaxUnavailable after Default = %d, want %d", got, defaultMaxUnavailable)
	}
}

func TestCCEManagedMachinePoolTemplateDefaultKeepsExplicitValue(t *testing.T) {
	tpl := &CCEManagedMachinePoolTemplate{
		Spec: CCEManagedMachinePoolTemplateSpec{
			Template: CCEManagedMachinePoolTemplateResource{
				Spec: CCEManagedMachinePoolSpec{UpdateConfig: UpdateConfigSpec{MaxUnavailable: 3}},
			},
		},
	}
	if err := tpl.Default(context.Background(), tpl); err != nil {
		t.Fatalf("CCEManagedMachinePoolTemplate.Default() error = %v, want nil", err)
	}
	if got := tpl.Spec.Template.Spec.UpdateConfig.MaxUnavailable; got != 3 {
		t.Errorf("MaxUnavailable after Default = %d, want 3", got)
	}
}

func TestCCEManagedMachinePoolTemplateValidateDeleteAllows(t *testing.T) {
	warnings, err := (&CCEManagedMachinePoolTemplate{}).ValidateDelete(context.Background(), &CCEManagedMachinePoolTemplate{})
	if err != nil || len(warnings) != 0 {
		t.Errorf("CCEManagedMachinePoolTemplate.ValidateDelete() = (%v, %v), want (nil, nil)", warnings, err)
	}
}

func TestCCEManagedMachinePoolValidateDeleteAllows(t *testing.T) {
	warnings, err := (&CCEManagedMachinePool{}).ValidateDelete(context.Background(), &CCEManagedMachinePool{})
	if err != nil || len(warnings) != 0 {
		t.Errorf("CCEManagedMachinePool.ValidateDelete() = (%v, %v), want (nil, nil)", warnings, err)
	}
}
