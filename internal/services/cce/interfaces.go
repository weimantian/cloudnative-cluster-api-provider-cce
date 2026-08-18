/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package cce provides the interface and Huawei Cloud SDK implementation for
// the CCE service (clusters, node pools, kubeconfig). Controllers depend only
// on this interface, so tests can inject mocks (pattern: CAPA
// pkg/cloud/services interfaces).
package cce

import "context"

// Endpoint is an API server endpoint of a CCE cluster.
type Endpoint struct {
	// URL of the endpoint.
	URL string
	// Type: "public" or "private" (official ShowClusterEndpoints model).
	Type string
}

// ClusterInfo is the provider-side representation of a CCE cluster.
type ClusterInfo struct {
	// ClusterID is the CCE cluster UUID.
	ClusterID string
	// Phase is the CCE cluster status (Available/Unavailable/ScalingUp/
	// ScalingDown/Creating ... official model).
	Phase string
	// Version of the cluster as reported by CCE.
	Version string
	// Endpoints of the cluster API server.
	Endpoints []Endpoint
}

// CreateClusterInput maps the CCEManagedControlPlane spec to the CCE
// CreateCluster API.
type CreateClusterInput struct {
	Name                 string
	Category             string // CCE | Turbo
	Flavor               string
	Version              string
	ContainerNetworkMode string // overlay_l2 | vpc-router | eni
	ContainerNetworkCIDR string
	ENISubnets           []string
	ServiceCIDR          string
	CustomSAN            []string
	PublicAccess         bool
	AgencyName           string
	BillingMode          int32
	Tags                 map[string]string
}

// CreateNodePoolInput maps the CCEManagedMachinePool spec to the CCE
// CreateNodePool API (nodeTemplate + initialNodeCount).
type CreateNodePoolInput struct {
	ClusterID        string
	Name             string
	Flavor           string
	OS               string
	RootVolumeSize   int32
	RootVolumeType   string
	DataVolumeSize   int32
	DataVolumeType   string
	SSHKey           string
	AvailabilityZone string
	InitialNodeCount int32
	BillingMode      int32
	Taints           []string // "key=value:effect"
	Labels           map[string]string
	SecurityGroups   []string
	Tags             map[string]string
}

// NodePoolInfo is the provider-side representation of a CCE node pool.
type NodePoolInfo struct {
	NodePoolID string
	Name       string
	// DesiredNodeCount as reported by CCE.
	DesiredNodeCount int32
	// NodeCount is the number of nodes in the pool (Active count, subject to
	// verification — questionnaire Q3).
	NodeCount int32
}

// Service is the CCE API surface consumed by the provider controllers.
type Service interface {
	// ShowCluster returns the current state of a CCE cluster.
	ShowCluster(ctx context.Context, clusterID string) (*ClusterInfo, error)
	// CreateCluster creates a CCE cluster and returns its ID.
	CreateCluster(ctx context.Context, in CreateClusterInput) (string, error)
	// DeleteCluster deletes a CCE cluster.
	DeleteCluster(ctx context.Context, clusterID string) error
	// GetClusterKubeconfig downloads and assembles the cluster kubeconfig.
	GetClusterKubeconfig(ctx context.Context, clusterID string, durationDays int32) (string, error)
	// CreateNodePool creates a node pool and returns its ID.
	CreateNodePool(ctx context.Context, in CreateNodePoolInput) (string, error)
	// ScaleNodePool scales a node pool to the given absolute desired total
	// node count (desiredNodeCount semantics per official docs — see
	// questionnaire Q3 for the pending live-test confirmation).
	ScaleNodePool(ctx context.Context, clusterID, nodePoolID string, desiredCount int32) error
	// DeleteNodePool deletes a node pool.
	DeleteNodePool(ctx context.Context, clusterID, nodePoolID string) error
	// ListNodePools lists the node pools of a cluster.
	ListNodePools(ctx context.Context, clusterID string) ([]NodePoolInfo, error)
}
