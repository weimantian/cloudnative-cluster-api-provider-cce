/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"testing"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
)

// TestToCreateNodePoolInputSpotAndMultiAZ verifies spot (竞价) and extension
// scale group (multi-AZ) fields are forwarded to the service layer.
func TestToCreateNodePoolInputSpotAndMultiAZ(t *testing.T) {
	pool := &infrav1beta1.CCEManagedMachinePool{
		Spec: infrav1beta1.CCEManagedMachinePoolSpec{
			Spot:      true,
			SpotPrice: "0.5",
			ExtensionScaleGroups: []infrav1beta1.ExtensionScaleGroupSpec{
				{Name: "az-b", Flavor: "c7.large.2", AvailabilityZone: "cn-north-4b"},
				{Name: "az-c", Flavor: "c7.large.2", AvailabilityZone: "cn-north-4c"},
			},
		},
	}
	in := toCreateNodePoolInput("cluster-1", pool)
	if !in.Spot || in.SpotPrice != "0.5" {
		t.Errorf("expected spot=true price=0.5, got spot=%v price=%s", in.Spot, in.SpotPrice)
	}
	if len(in.ExtensionScaleGroups) != 2 {
		t.Fatalf("expected 2 extension scale groups, got %d", len(in.ExtensionScaleGroups))
	}
	if in.ExtensionScaleGroups[0].Name != "az-b" || in.ExtensionScaleGroups[0].AvailabilityZone != "cn-north-4b" {
		t.Errorf("unexpected first group: %+v", in.ExtensionScaleGroups[0])
	}
}

// TestToCreateNodePoolInputMultipleDataVolumes verifies all declared data
// volumes are forwarded to the service layer (previously only the first was
// mapped and the webhook rejected more than one).
func TestToCreateNodePoolInputMultipleDataVolumes(t *testing.T) {
	pool := &infrav1beta1.CCEManagedMachinePool{
		Spec: infrav1beta1.CCEManagedMachinePoolSpec{
			DataVolumes: []common.NodeVolume{
				{Size: 100, Type: "GPSSD"},
				{Size: 500, Type: "SSD"},
			},
		},
	}
	in := toCreateNodePoolInput("cluster-1", pool)
	if len(in.DataVolumes) != 2 {
		t.Fatalf("expected 2 data volumes, got %d", len(in.DataVolumes))
	}
	if in.DataVolumes[0].Size != 100 || in.DataVolumes[0].Type != "GPSSD" {
		t.Errorf("unexpected first data volume: %+v", in.DataVolumes[0])
	}
	if in.DataVolumes[1].Size != 500 || in.DataVolumes[1].Type != "SSD" {
		t.Errorf("unexpected second data volume: %+v", in.DataVolumes[1])
	}

	// Empty data volumes -> empty slice (no data volume sent).
	empty := toCreateNodePoolInput("cluster-1", &infrav1beta1.CCEManagedMachinePool{})
	if len(empty.DataVolumes) != 0 {
		t.Errorf("expected no data volumes, got %d", len(empty.DataVolumes))
	}
}

// TestToCreateClusterInputPublicAccessCIDRs verifies the public access
// whitelist CIDRs are forwarded to the service layer.
func TestToCreateClusterInputPublicAccessCIDRs(t *testing.T) {
	cp := &controlplanev1beta1.CCEManagedControlPlane{
		Spec: controlplanev1beta1.CCEManagedControlPlaneSpec{
			ClusterName: "test-cluster",
			EndpointAccess: controlplanev1beta1.EndpointAccessSpec{
				Public: true,
				CIDRs:  []string{"10.0.0.0/8", "172.16.0.0/12"},
			},
		},
	}
	in := toCreateClusterInput(cp, "vpc-1", "sub-1", "")
	if !in.PublicAccess {
		t.Error("expected PublicAccess true")
	}
	if len(in.PublicAccessCIDRs) != 2 || in.PublicAccessCIDRs[0] != "10.0.0.0/8" || in.PublicAccessCIDRs[1] != "172.16.0.0/12" {
		t.Errorf("unexpected PublicAccessCIDRs: %v", in.PublicAccessCIDRs)
	}

	// No cidrs -> empty (platform default 0.0.0.0/0).
	cp2 := &controlplanev1beta1.CCEManagedControlPlane{
		Spec: controlplanev1beta1.CCEManagedControlPlaneSpec{
			ClusterName:    "test-cluster",
			EndpointAccess: controlplanev1beta1.EndpointAccessSpec{Public: true},
		},
	}
	if in2 := toCreateClusterInput(cp2, "vpc-1", "sub-1", ""); len(in2.PublicAccessCIDRs) != 0 {
		t.Errorf("expected empty PublicAccessCIDRs, got %v", in2.PublicAccessCIDRs)
	}
}
