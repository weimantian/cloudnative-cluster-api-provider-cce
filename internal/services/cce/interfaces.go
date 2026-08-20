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

// Endpoint is an API server endpoint of a CCE cluster. It mirrors
// Cluster.status.endpoints from ShowCluster (NOT the ShowClusterEndpoints API,
// which returns privateEndpoint/publicEndpoint strings).
type Endpoint struct {
	// URL of the endpoint.
	URL string
	// Type: "Internal" (VPC-internal) or "External" (public) per the official
	// ClusterEndpoints model in ShowCluster.
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
	// HostNetworkVpcID is the VPC the cluster nodes live in (official
	// hostNetwork.vpc is required — A2).
	HostNetworkVpcID string
	// HostNetworkSubnetID is the node subnet (official hostNetwork.subnet).
	HostNetworkSubnetID string
	ServiceCIDR         string
	CustomSAN           []string
	PublicAccess        bool
	// PublicAccessCIDRs is the public API server whitelist (PublicAccess
	// model cidrs). Only sent when PublicAccess is true; empty = the
	// platform default ["0.0.0.0/0"].
	PublicAccessCIDRs []string
	AgencyName        string
	BillingMode       int32
	// PeriodType/PeriodNum are REQUIRED when BillingMode=1 (subscription):
	// periodType month|year, periodNum month [1-9] / year [1-3] (official
	// ClusterExtendParam; verified against CreateCluster.txt).
	PeriodType  string
	PeriodNum   int32
	IsAutoRenew string // "true" | "false"
	IsAutoPay   string // "true" | "false"
	// Tags are additional cluster tags (mapped to CCE clusterTags); the owned
	// tag (cluster-api-provider-cce.cluster.<name>=owned) is always added by
	// the service.
	Tags map[string]string
}

// CreateNodePoolInput maps the CCEManagedMachinePool spec to the CCE
// CreateNodePool API (nodeTemplate + initialNodeCount).
type CreateNodePoolInput struct {
	ClusterID      string
	Name           string
	Flavor         string
	OS             string
	RootVolumeSize int32
	RootVolumeType string
	// DataVolumes of the nodes (nodeTemplate.dataVolumes); each entry maps
	// to a model.Volume{Size, Volumetype}. Empty = no data volumes.
	DataVolumes      []NodeVolumeInput
	SSHKey           string
	AvailabilityZone string
	InitialNodeCount int32
	BillingMode      int32
	// Spot requests spot (竞价) instances (nodeTemplate.extendParam.
	// marketType=spot). Only effective with BillingMode=0.
	Spot bool
	// SpotPrice is the max hourly price for spot instances; empty = the
	// on-demand price is used as the spot price.
	SpotPrice string
	// ExtensionScaleGroups extends the pool into additional AZs (CCE
	// 扩展伸缩组); each carries its own flavor/AZ.
	ExtensionScaleGroups []ExtensionScaleGroupInput
	Taints               []string // "key=value:effect"
	Labels               map[string]string
	SecurityGroups       []string
	// CustomSecurityGroups to bind to newly scaled nodes (Q5).
	CustomSecurityGroups []string
	// Autoscaling maps to NodePoolNodeAutoscaling (feature gate
	// NodePoolAutoscaling; nil = autoscaling disabled).
	Autoscaling *NodePoolAutoscaling
	// ClusterName is the owning cluster name (for the owned tag).
	ClusterName string
	// Tags are additional node pool tags (mapped to CCE userTags); the owned
	// tag is always added by the service.
	Tags map[string]string
}

// NodePoolAutoscaling mirrors NodePoolNodeAutoscaling (enable/min/max).
type NodePoolAutoscaling struct {
	Enable       bool
	MinNodeCount int32
	MaxNodeCount int32
}

// NodeVolumeInput describes a root or data volume in the service layer.
// It is a projection of the API common.NodeVolume (Size + Type).
type NodeVolumeInput struct {
	Size int32
	Type string
}

// ExtensionScaleGroupInput describes one CCE extension scale group.
type ExtensionScaleGroupInput struct {
	Name             string
	Flavor           string
	AvailabilityZone string
}

// NodePoolInfo is the provider-side representation of a CCE node pool.
type NodePoolInfo struct {
	NodePoolID string
	Name       string
	// DesiredNodeCount as reported by CCE (spec.initialNodeCount).
	DesiredNodeCount int32
	// NodeCount is status.currentNode (expected total, incl. creating/
	// deleting) as reported by CCE.
	NodeCount int32
	// ActiveNodeCount is status.activeNode (nodes in Active state).
	ActiveNodeCount int32
}

// DeleteClusterInput carries the CCE DeleteCluster query options. Official
// defaults leave EVS/storage behind (delete_evs=false), so the provider
// explicitly requests deletion of on-demand resources (verified against
// cce_02_0241 — questionnaire Q8).
type DeleteClusterInput struct {
	ClusterID string
	// DeleteEVS deletes EVS volumes (official default: skip => leftovers).
	DeleteEVS bool
	// DeleteENI deletes ENI ports (official default: block).
	DeleteENI bool
	// DeleteELB deletes auto-created ELB / Service / Ingress resources
	// (official default: block).
	DeleteELB bool
	// DeleteEFS deletes SFS Turbo volumes (official default: skip).
	DeleteEFS bool
	// DeleteOBS deletes OBS buckets (official default: skip).
	DeleteOBS bool
	// DeleteSFS deletes SFS volumes (official default: skip).
	DeleteSFS bool
	// DeleteSFS30 deletes SFS 3.0 volumes (official default: skip).
	DeleteSFS30 bool
	// OnDemandNodePolicy: delete | reset | retain (official default: delete
	// on-demand nodes, retain admitted nodes).
	OnDemandNodePolicy string
	// PeriodicNodePolicy: reset | retain. Empty leaves the parameter unset so
	// the official default (retain) applies.
	PeriodicNodePolicy string
}

// UpdateNodePoolInput carries the fields updated on a node pool.
type UpdateNodePoolInput struct {
	ClusterID  string
	NodePoolID string
	// InitialNodeCount is the new expected node count (official: required,
	// defaults to 0 which shrinks the pool — Q3).
	InitialNodeCount int32
	// IgnoreInitialNodeCount leaves the expected count untouched.
	IgnoreInitialNodeCount bool
	// CustomSecurityGroups to apply to newly scaled nodes (Q5). An empty slice
	// resets to the node default security group (official: "未指定安全组ID,
	// 新建节点将添加Node节点默认安全组").
	CustomSecurityGroups []string
	// Autoscaling maps to NodePoolNodeAutoscaling (feature gate
	// NodePoolAutoscaling; nil = leave autoscaling untouched).
	Autoscaling *NodePoolAutoscaling
	// TaintPolicyOnExistingNodes: "refresh" syncs spec taints to existing
	// nodes, "ignore" leaves them (official NodePoolSpecUpdate).
	TaintPolicyOnExistingNodes string
	// LabelPolicyOnExistingNodes: "refresh" syncs spec labels to existing
	// nodes, "ignore" leaves them.
	LabelPolicyOnExistingNodes string
	// UserTagsPolicyOnExistingNodes: "refresh" | "ignore" for user tags.
	UserTagsPolicyOnExistingNodes string
}

// QuotaInfo is the cluster quota for the project.
type QuotaInfo struct {
	// ClusterQuotaLimit is the max number of clusters (official: per region).
	ClusterQuotaLimit int32
	// ClusterQuotaUsed is the number of clusters in use.
	ClusterQuotaUsed int32
}

// UpgradeInfo is the platform's upgrade information for a cluster
// (ShowClusterUpgradeInfo). TargetVersions empty means the platform currently
// offers no upgrade path from the running version (questionnaire Q11: verified
// live — v1.34.8-r2 returns an empty list).
type UpgradeInfo struct {
	// CurrentVersion is the running release, e.g. "v1.34.8".
	CurrentVersion string
	// Patch is the running patch, e.g. "r2".
	Patch string
	// SuggestPatch is the patch the platform recommends upgrading to first
	// (official GetClusterUpgradeInfo: "推荐升级的目标补丁版本号,如r0"). Empty
	// when no patch upgrade is suggested.
	SuggestPatch string
	// TargetVersions are the versions the platform offers as upgrade targets.
	TargetVersions []string
}

// UpgradeTaskPhase values reported by ShowUpgradeClusterTask
// (UpgradeTaskStatus.Phase): Init/Queuing/Running/Pause/Success/Failed.
const (
	UpgradeTaskPhaseSuccess = "Success"
	UpgradeTaskPhaseFailed  = "Failed"
)

// AddonInput declares a CCE addon instance to install/update (maps to
// CreateAddonInstance / UpdateAddonInstance).
type AddonInput struct {
	ClusterID string
	// AddonID is the addon INSTANCE id (returned by Create/List), used for
	// Update/Delete. Empty for Create.
	AddonID string
	// Name is the addon template name (e.g. "coredns", "metrics-server").
	Name string
	// Version is the addon template version (e.g. "1.0.0"); empty means the
	// latest supported by the cluster.
	Version string
	// Values are the per-addon install parameters (optional).
	Values map[string]interface{}
}

// PodIdentityAssociationInput declares a CCE pod-identity association
// (binds a K8s ServiceAccount to a Huawei Cloud agency — the CCE equivalent
// of EKS Pod Identity).
type PodIdentityAssociationInput struct {
	ClusterID      string
	Namespace      string
	ServiceAccount string
	AgencyName     string
	// AssociationID is the pod-identity association ID (for Delete).
	AssociationID string
}

// PodIdentityAssociationInfo is a pod-identity association as reported by
// ListPodIdentityAssociations.
type PodIdentityAssociationInfo struct {
	ID             string
	Namespace      string
	ServiceAccount string
	AgencyName     string
}

// AddonInfo is a CCE addon instance as reported by ListAddonInstances.
type AddonInfo struct {
	// ID is the addon instance ID.
	ID string
	// Name is the addon template name.
	Name string
	// Version is the installed addon version.
	Version string
	// Status is the addon instance status (running/installing/upgrading/...).
	Status string
}

// LogConfigInput declares one control-plane log collection item.
type LogConfigInput struct {
	// Name is the log type (kube-apiserver/kube-controller-manager/
	// kube-scheduler/audit, or a system addon name).
	Name string
	// Type is the component type: control, audit, or system-addon.
	Type string
	// Enable turns collection on/off.
	Enable bool
}

// LogConfigInfo is the current cluster log configuration as reported by
// ShowClusterConfig.
type LogConfigInfo struct {
	// TTLInDays is the log retention in days (0-30).
	TTLInDays int32
	// Logs lists the configured log items.
	Logs []LogConfigInput
}

// ClusterRef is a minimal reference to a CCE cluster as returned by
// ListClusters, used by the garbage collector to detect orphaned clusters.
type ClusterRef struct {
	ClusterID string
	Name      string
	Tags      map[string]string
}

// Service is the CCE API surface consumed by the provider controllers.
type Service interface { // ShowCluster returns the current state of a CCE cluster.
	ShowCluster(ctx context.Context, clusterID string) (*ClusterInfo, error)
	// CreateCluster creates a CCE cluster and returns its ID.
	CreateCluster(ctx context.Context, in CreateClusterInput) (string, error)
	// DeleteCluster deletes a CCE cluster with the given delete options.
	DeleteCluster(ctx context.Context, in DeleteClusterInput) error
	// GetClusterKubeconfig downloads and assembles the cluster kubeconfig.
	GetClusterKubeconfig(ctx context.Context, clusterID string, durationDays int32) (string, error)
	// ShowQuotas returns the project cluster quota (ShowQuotas API).
	ShowQuotas(ctx context.Context) (*QuotaInfo, error)
	// ListClusters lists all CCE clusters in the region (used by the garbage
	// collector's orphan sweeper; returns cluster ID, name and tags).
	ListClusters(ctx context.Context) ([]ClusterRef, error)
	// CreateNodePool creates a node pool and returns its ID.
	CreateNodePool(ctx context.Context, in CreateNodePoolInput) (string, error)
	// ScaleNodePool scales a node pool to the given absolute desired total
	// node count (desiredNodeCount semantics per official docs — see
	// questionnaire Q3 for the pending live-test confirmation).
	ScaleNodePool(ctx context.Context, clusterID, nodePoolID string, desiredCount int32) error
	// UpdateNodePool updates a node pool. When ignoreInitialNodeCount is true
	// the pool's expected count is left untouched (official: omitting
	// initialNodeCount defaults it to 0 and shrinks the pool to 0 — Q3).
	UpdateNodePool(ctx context.Context, in UpdateNodePoolInput) error
	// DeleteNodePool deletes a node pool.
	DeleteNodePool(ctx context.Context, clusterID, nodePoolID string) error
	// ListNodePools lists the node pools of a cluster.
	ListNodePools(ctx context.Context, clusterID string) ([]NodePoolInfo, error)
	// GetUpgradeInfo returns the platform's upgrade targets for a cluster.
	GetUpgradeInfo(ctx context.Context, clusterID string) (*UpgradeInfo, error)
	// StartUpgrade drives the upgrade workflow (CreateUpgradeWorkFlow ->
	// CreatePreCheck -> UpgradeCluster) and returns the upgrade task ID used
	// by ShowUpgradeTask.
	StartUpgrade(ctx context.Context, clusterID, targetVersion string) (string, error)
	// ShowUpgradeTask returns the upgrade task phase (Init/Queuing/Running/
	// Pause/Success/Failed).
	ShowUpgradeTask(ctx context.Context, clusterID, taskID string) (string, error)
	// CreateAddonInstance installs a CCE addon and returns its instance ID.
	CreateAddonInstance(ctx context.Context, in AddonInput) (string, error)
	// UpdateAddonInstance upgrades an existing addon to the given version.
	UpdateAddonInstance(ctx context.Context, in AddonInput) error
	// ListAddonInstances lists the addon instances of a cluster.
	ListAddonInstances(ctx context.Context, clusterID string) ([]AddonInfo, error)
	// DeleteAddonInstance removes an addon instance.
	DeleteAddonInstance(ctx context.Context, clusterID, addonID string) error
	// CreatePodIdentityAssociation binds a ServiceAccount to an agency and
	// returns the association ID.
	CreatePodIdentityAssociation(ctx context.Context, in PodIdentityAssociationInput) (string, error)
	// ListPodIdentityAssociations lists the pod-identity associations.
	ListPodIdentityAssociations(ctx context.Context, clusterID string) ([]PodIdentityAssociationInfo, error)
	// DeletePodIdentityAssociation removes an association by ID.
	DeletePodIdentityAssociation(ctx context.Context, clusterID, associationID string) error
	// UpgradeNodePool rolls the node pool's configuration onto existing nodes
	// (CCE 同步节点池), maxUnavailable nodes at a time (1-20).
	UpgradeNodePool(ctx context.Context, clusterID, nodePoolID string, maxUnavailable int32) error
	// UpdateClusterLogConfig applies the control-plane log collection config.
	UpdateClusterLogConfig(ctx context.Context, clusterID string, ttlInDays int32, logs []LogConfigInput) error
	// ShowClusterLogConfig returns the current control-plane log config.
	ShowClusterLogConfig(ctx context.Context, clusterID string) (*LogConfigInfo, error)
}
