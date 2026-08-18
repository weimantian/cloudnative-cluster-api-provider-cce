/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package common contains types shared across the infrastructure and
// controlplane API groups.
// +kubebuilder:object:generate=true
package common

// VPC references an existing or to-be-created Huawei Cloud VPC.
// If ID is set, the provider references an existing VPC and never modifies it.
// Otherwise the provider may create a VPC with the given name/CIDR and record
// the created resource in ResourceID/UID (mirrors the "reference vs create"
// dual-mode design of the Alibaba ACK provider).
type VPC struct {
	// ID of an existing VPC. If set, the VPC is referenced and not managed.
	// +optional
	ID string `json:"id,omitempty"`

	// Name of the VPC to create (only used when ID is empty).
	// +optional
	Name string `json:"name,omitempty"`

	// CIDR of the VPC to create (only used when ID is empty).
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// ResourceID records the created VPC resource ID (provider-managed).
	// +optional
	ResourceID string `json:"resourceID,omitempty"`

	// Description of the VPC to create.
	// +optional
	Description string `json:"description,omitempty"`
}

// Subnet references an existing or to-be-created subnet.
type Subnet struct {
	// ID of an existing subnet. If set, the subnet is referenced and not managed.
	// +optional
	ID string `json:"id,omitempty"`

	// Name of the subnet to create (only used when ID is empty).
	// +optional
	Name string `json:"name,omitempty"`

	// CIDR of the subnet to create (only used when ID is empty).
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// VPCID of the VPC the subnet belongs to (only used when creating).
	// +optional
	VPCID string `json:"vpcId,omitempty"`

	// AvailabilityZone of the subnet (only used when creating).
	// +optional
	AvailabilityZone string `json:"availabilityZone,omitempty"`

	// ResourceID records the created subnet resource ID (provider-managed).
	// +optional
	ResourceID string `json:"resourceID,omitempty"`
}

// NetworkSpec describes the network a CCE cluster consumes. CCE does not create
// the network; it references VPC/subnets that must exist before cluster
// creation (official CreateCluster prerequisite).
type NetworkSpec struct {
	// VPC referenced by the cluster.
	// +optional
	VPC VPC `json:"vpc,omitempty"`

	// Subnets referenced by the cluster (node subnets / ENI subnets for Turbo).
	// +optional
	Subnets []Subnet `json:"subnets,omitempty"`
}

// NodeVolume describes a root or data volume of a node.
type NodeVolume struct {
	// Size in GiB.
	// +kubebuilder:validation:Minimum=1
	Size int32 `json:"size"`

	// Type of the volume, e.g. GPSSD / SSD / SAS.
	// +optional
	Type string `json:"type,omitempty"`
}

// Tags is a set of key/value labels attached to cloud resources (CCE cluster,
// node pool) for ownership identification (CAPI contract: identification must
// not rely on naming alone).
type Tags map[string]string
