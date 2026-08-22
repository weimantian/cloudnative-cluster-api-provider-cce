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

	// Tags attached to the VPC. The provider owned tag
	// (cluster-api-provider-cce.cluster.<name>=owned) marks an existing VPC
	// as ADOPTED (managed, including deletion) - the CAPA three-state model:
	// vpc.id empty = create, vpc.id + owned tag = adopt, vpc.id + no tag = BYO.
	// +optional
	Tags Tags `json:"tags,omitempty"`
}

// SubnetType classifies a subnet's role for managed networks.
type SubnetType string

const (
	// SubnetTypeNode is a node (host) subnet.
	SubnetTypeNode SubnetType = "node"
	// SubnetTypeENI is a Turbo container (ENI) subnet.
	SubnetTypeENI SubnetType = "eni"
)

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

	// Type of the subnet: node (default) or eni (Turbo container subnet).
	// Managed Turbo clusters derive the ENI neutron_subnet_id from
	// subnets of Type eni.
	// +optional
	Type SubnetType `json:"type,omitempty"`
	// NeutronSubnetID records the neutron subnet ID (provider-managed; the
	// CCE eniNetwork API requires the neutron ID, not the network ID).
	// +optional
	NeutronSubnetID string `json:"neutronSubnetId,omitempty"`

	// ResourceID records the created subnet resource ID (provider-managed).
	// +optional
	ResourceID string `json:"resourceID,omitempty"`
}

// NetworkSpec describes the network a CCE cluster consumes. Two modes:
//
//   - managed (vpc.id empty): the provider creates the VPC, subnets and -
//     when natGateway is enabled - a NAT gateway with SNAT rules for node
//     egress, and records the created resource IDs in ResourceID fields;
//   - BYO (vpc.id set): the provider references the existing VPC/subnets and
//     never modifies them (CCE requires a VPC to exist before cluster
//     creation - official CreateCluster prerequisite).
type NetworkSpec struct {
	// VPC referenced (BYO) or created (managed) for the cluster.
	// +optional
	VPC VPC `json:"vpc,omitempty"`

	// Subnets referenced (BYO) or created (managed) by the cluster. When
	// empty in managed mode, a default node subnet is derived from the VPC
	// CIDR.
	// +optional
	Subnets []Subnet `json:"subnets,omitempty"`

	// NatGateway enables managed node egress (managed mode only): a NAT
	// gateway + EIP + SNAT rule per managed subnet. Ignored in BYO mode.
	// +optional
	NatGateway *NatGatewaySpec `json:"natGateway,omitempty"`
}

// NatGatewaySpec declares a managed NAT gateway for node egress. Its mere
// presence enables managed NAT (mirrors CAPA, which builds a NAT gateway by
// default whenever a managed VPC has private subnets - there is no enabled
// switch; BYO or an omitted natGateway disables it).
type NatGatewaySpec struct {
	// Spec of the NAT gateway: "1" (small, default), "2", "3", "4".
	// +kubebuilder:default="1"
	// +optional
	Spec string `json:"spec,omitempty"`

	// ResourceID records the created NAT gateway ID (provider-managed).
	// +optional
	ResourceID string `json:"resourceID,omitempty"`

	// EIPResourceID records the created NAT EIP ID (provider-managed).
	// +optional
	EIPResourceID string `json:"eipResourceID,omitempty"`
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
