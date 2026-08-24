/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"testing"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// TestToCreateNodePoolInputSpotAndMultiAZ verifies spot (竞价) and extension
// scale group (multi-AZ) fields are forwarded to the service layer.
func TestToCreateNodePoolInputSpotAndMultiAZ(t *testing.T) {
	pool := &infrav1beta2.CCEManagedMachinePool{
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			Spot:      true,
			SpotPrice: "0.5",
			ExtensionScaleGroups: []infrav1beta2.ExtensionScaleGroupSpec{
				{Name: "az-b", Flavor: "c7.large.2", AvailabilityZone: "cn-north-4b"},
				{Name: "az-c", Flavor: "c7.large.2", AvailabilityZone: "cn-north-4c"},
			},
		},
	}
	in := toCreateNodePoolInput("cluster-1", pool, nil)
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
	pool := &infrav1beta2.CCEManagedMachinePool{
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			DataVolumes: []common.NodeVolume{
				{Size: 100, Type: "GPSSD"},
				{Size: 500, Type: "SSD"},
			},
		},
	}
	in := toCreateNodePoolInput("cluster-1", pool, nil)
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
	empty := toCreateNodePoolInput("cluster-1", &infrav1beta2.CCEManagedMachinePool{}, nil)
	if len(empty.DataVolumes) != 0 {
		t.Errorf("expected no data volumes, got %d", len(empty.DataVolumes))
	}
}

// TestToCreateNodePoolInputLaunchTemplate verifies the launch-template
// analogues (ecsGroupId/faultDomain/dedicatedHostId) are forwarded to the
// service layer.
func TestToCreateNodePoolInputLaunchTemplate(t *testing.T) {
	pool := &infrav1beta2.CCEManagedMachinePool{
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			EcsGroupId:      "ecs-group-1",
			FaultDomain:     "fault-domain-1",
			DedicatedHostId: "dh-1",
		},
	}
	in := toCreateNodePoolInput("cluster-1", pool, nil)
	if in.EcsGroupId != "ecs-group-1" {
		t.Errorf("expected EcsGroupId=ecs-group-1, got %s", in.EcsGroupId)
	}
	if in.FaultDomain != "fault-domain-1" {
		t.Errorf("expected FaultDomain=fault-domain-1, got %s", in.FaultDomain)
	}
	if in.DedicatedHostId != "dh-1" {
		t.Errorf("expected DedicatedHostId=dh-1, got %s", in.DedicatedHostId)
	}

	// Empty launch-template fields -> empty strings (omitted by SDK mapping).
	empty := toCreateNodePoolInput("cluster-1", &infrav1beta2.CCEManagedMachinePool{}, nil)
	if empty.EcsGroupId != "" || empty.FaultDomain != "" || empty.DedicatedHostId != "" {
		t.Errorf("expected empty launch-template fields, got %s/%s/%s",
			empty.EcsGroupId, empty.FaultDomain, empty.DedicatedHostId)
	}
}

// TestToCreateClusterInputPublicAccessCIDRs verifies the public access
// whitelist CIDRs are forwarded to the service layer.
func TestToCreateClusterInputPublicAccessCIDRs(t *testing.T) {
	cp := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName: "test-cluster",
			EndpointAccess: controlplanev1beta2.EndpointAccessSpec{
				Public: true,
				CIDRs:  []string{"10.0.0.0/8", "172.16.0.0/12"},
			},
		},
	}
	in := toCreateClusterInput(cp, "vpc-1", "sub-1", "", nil)
	if !in.PublicAccess {
		t.Error("expected PublicAccess true")
	}
	if len(in.PublicAccessCIDRs) != 2 || in.PublicAccessCIDRs[0] != "10.0.0.0/8" || in.PublicAccessCIDRs[1] != "172.16.0.0/12" {
		t.Errorf("unexpected PublicAccessCIDRs: %v", in.PublicAccessCIDRs)
	}

	// No cidrs -> empty (platform default 0.0.0.0/0).
	cp2 := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName:    "test-cluster",
			EndpointAccess: controlplanev1beta2.EndpointAccessSpec{Public: true},
		},
	}
	if in2 := toCreateClusterInput(cp2, "vpc-1", "sub-1", "", nil); len(in2.PublicAccessCIDRs) != 0 {
		t.Errorf("expected empty PublicAccessCIDRs, got %v", in2.PublicAccessCIDRs)
	}
}

// TestToCreateClusterInputSecondaryCIDR verifies the secondary container
// CIDRs (model.ContainerNetwork.Cidrs) are forwarded to the service layer.
func TestToCreateClusterInputSecondaryCIDR(t *testing.T) {
	cp := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName: "test-cluster",
			ContainerNetwork: controlplanev1beta2.ContainerNetworkSpec{
				CIDR:  "10.0.0.0/16",
				CIDRs: []string{"10.1.0.0/16", "10.2.0.0/16"},
			},
		},
	}
	in := toCreateClusterInput(cp, "vpc-1", "sub-1", "", nil)
	if in.ContainerNetworkCIDR != "10.0.0.0/16" {
		t.Errorf("expected ContainerNetworkCIDR=10.0.0.0/16, got %s", in.ContainerNetworkCIDR)
	}
	if len(in.ContainerNetworkCIDRs) != 2 || in.ContainerNetworkCIDRs[0] != "10.1.0.0/16" || in.ContainerNetworkCIDRs[1] != "10.2.0.0/16" {
		t.Errorf("unexpected ContainerNetworkCIDRs: %v", in.ContainerNetworkCIDRs)
	}

	// No secondary CIDRs -> empty slice (single-CIDR cluster).
	cp2 := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName:      "test-cluster",
			ContainerNetwork: controlplanev1beta2.ContainerNetworkSpec{CIDR: "10.0.0.0/16"},
		},
	}
	if in2 := toCreateClusterInput(cp2, "vpc-1", "sub-1", "", nil); len(in2.ContainerNetworkCIDRs) != 0 {
		t.Errorf("expected empty ContainerNetworkCIDRs, got %v", in2.ContainerNetworkCIDRs)
	}
}

// TestToCreateClusterInputIPv6DualStack verifies IPv6 enable flag and the
// service network IPv6 CIDR are forwarded to the service layer.
func TestToCreateClusterInputIPv6DualStack(t *testing.T) {
	v := true
	cp := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName: "test-cluster",
			Ipv6Enable:  &v,
			ServiceNetwork: controlplanev1beta2.ServiceNetworkSpec{
				CIDR:     "10.247.0.0/16",
				IPv6CIDR: "fd00::/108",
			},
		},
	}
	in := toCreateClusterInput(cp, "vpc-1", "sub-1", "", nil)
	if in.Ipv6Enable == nil || !*in.Ipv6Enable {
		t.Error("expected Ipv6Enable=true")
	}
	if in.ServiceIPv6CIDR != "fd00::/108" {
		t.Errorf("expected ServiceIPv6CIDR=fd00::/108, got %s", in.ServiceIPv6CIDR)
	}

	// No IPv6 -> nil enable, empty IPv6 CIDR.
	cp2 := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName:    "test-cluster",
			ServiceNetwork: controlplanev1beta2.ServiceNetworkSpec{CIDR: "10.247.0.0/16"},
		},
	}
	in2 := toCreateClusterInput(cp2, "vpc-1", "sub-1", "", nil)
	if in2.Ipv6Enable != nil {
		t.Errorf("expected nil Ipv6Enable, got %v", *in2.Ipv6Enable)
	}
	if in2.ServiceIPv6CIDR != "" {
		t.Errorf("expected empty ServiceIPv6CIDR, got %s", in2.ServiceIPv6CIDR)
	}
}

// TestToCreateClusterInputEnableAutopilot verifies the EnableAutopilot flag
// is forwarded to the service layer.
func TestToCreateClusterInputEnableAutopilot(t *testing.T) {
	v := true
	cp := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
			ClusterName:     "test-cluster",
			EnableAutopilot: &v,
		},
	}
	in := toCreateClusterInput(cp, "vpc-1", "sub-1", "", nil)
	if in.EnableAutopilot == nil || !*in.EnableAutopilot {
		t.Error("expected EnableAutopilot=true")
	}

	// Default -> nil flag.
	cp2 := &controlplanev1beta2.CCEManagedControlPlane{
		Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{ClusterName: "test-cluster"},
	}
	if in2 := toCreateClusterInput(cp2, "vpc-1", "sub-1", "", nil); in2.EnableAutopilot != nil {
		t.Errorf("expected nil EnableAutopilot, got %v", *in2.EnableAutopilot)
	}
}

// TestToCreateNodePoolInputLifecycleHooks verifies the node lifecycle hook
// fields (preInstall/postInstall scripts, waitPostInstallFinish) are
// forwarded to the service layer.
func TestToCreateNodePoolInputLifecycleHooks(t *testing.T) {
	wait := true
	pool := &infrav1beta2.CCEManagedMachinePool{
		Spec: infrav1beta2.CCEManagedMachinePoolSpec{
			PreInstall:            "cHJlLWluc3RhbGw=",
			PostInstall:           "cG9zdC1pbnN0YWxs",
			WaitPostInstallFinish: &wait,
		},
	}
	in := toCreateNodePoolInput("cluster-1", pool, nil)
	if in.PreInstall != "cHJlLWluc3RhbGw=" {
		t.Errorf("expected PreInstall=cHJlLWluc3RhbGw=, got %s", in.PreInstall)
	}
	if in.PostInstall != "cG9zdC1pbnN0YWxs" {
		t.Errorf("expected PostInstall=cG9zdC1pbnN0YWxs, got %s", in.PostInstall)
	}
	if in.WaitPostInstallFinish == nil || !*in.WaitPostInstallFinish {
		t.Errorf("expected WaitPostInstallFinish=true, got %v", in.WaitPostInstallFinish)
	}

	// Empty lifecycle hooks -> empty strings, nil flag.
	empty := toCreateNodePoolInput("cluster-1", &infrav1beta2.CCEManagedMachinePool{}, nil)
	if empty.PreInstall != "" || empty.PostInstall != "" {
		t.Errorf("expected empty lifecycle hooks, got %s/%s", empty.PreInstall, empty.PostInstall)
	}
	if empty.WaitPostInstallFinish != nil {
		t.Errorf("expected nil WaitPostInstallFinish, got %v", *empty.WaitPostInstallFinish)
	}
}
