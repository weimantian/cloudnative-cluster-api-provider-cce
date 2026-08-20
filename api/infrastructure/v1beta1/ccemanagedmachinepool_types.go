/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

// CCEManagedMachinePoolSpec defines the desired state of CCEManagedMachinePool.
// It maps 1:1 to a CCE node pool (NodePool). Field semantics follow the
// official CreateNodePool API (nodeTemplate / initialNodeCount / autoscaling).
type CCEManagedMachinePoolSpec struct {
	// ClusterName of the owning CCE cluster (matches Cluster name).
	// +kubebuilder:validation:Required
	ClusterName string `json:"clusterName"`

	// NodePoolName is the CCE node pool name.
	// +kubebuilder:validation:Required
	NodePoolName string `json:"nodePoolName"`

	// Flavor is the ECS instance flavor of the nodes (nodeTemplate.flavor).
	// +kubebuilder:validation:Required
	Flavor string `json:"flavor"`

	// OS image family of the nodes (nodeTemplate.os), e.g.
	// "Huawei Cloud EulerOS 2.0". Required by the webhook (official
	// CreateNodePool requires os unless a private image is used).
	// +optional
	OS string `json:"os,omitempty"`

	// RootVolume of the nodes (nodeTemplate.rootVolume). Pointer so an empty
	// value is omitted; required by the webhook (size 40-1024 GiB).
	// +optional
	RootVolume *common.NodeVolume `json:"rootVolume,omitempty"`

	// DataVolumes of the nodes (nodeTemplate.dataVolumes).
	// +optional
	DataVolumes []common.NodeVolume `json:"dataVolumes,omitempty"`

	// SSHKey used to access the nodes (nodeTemplate.sshKey).
	// +optional
	SSHKey string `json:"sshKey,omitempty"`

	// AvailabilityZone of the nodes. Required by the webhook (CCE does not
	// support random AZ via API).
	// +optional
	AvailabilityZone string `json:"availabilityZone,omitempty"`

	// Replicas is the desired node count (maps to the node pool expected count).
	// It is normally driven by the owning MachinePool.spec.replicas.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// BillingMode: 0=on-demand, 1=subscription.
	// +kubebuilder:validation:Enum=0;1
	// +optional
	BillingMode int32 `json:"billingMode,omitempty"`

	// Spot requests spot (竞价) instances for the node pool. Only effective
	// when billingMode=0 (on-demand); maps to nodeTemplate.extendParam.
	// marketType=spot.
	// +optional
	Spot bool `json:"spot,omitempty"`

	// SpotPrice is the maximum hourly price the user is willing to pay for
	// spot instances. Empty = the on-demand price is used as the spot price.
	// Only effective when spot is set and billingMode=0.
	// +optional
	SpotPrice string `json:"spotPrice,omitempty"`

	// ExtensionScaleGroups extends the node pool into additional availability
	// zones (CCE 扩展伸缩组). Each entry carries its own flavor and AZ; the
	// base nodeTemplate.az remains the primary AZ.
	// +optional
	ExtensionScaleGroups []ExtensionScaleGroupSpec `json:"extensionScaleGroups,omitempty"`

	// Taints applied to the nodes (max 20 per official constraint).
	// +kubebuilder:validation:MaxItems=20
	// +optional
	Taints []string `json:"taints,omitempty"`

	// Labels applied to the nodes.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// SecurityGroups to bind to the node pool (Turbo >= 1.21, max 5 per
	// official constraint).
	// +kubebuilder:validation:MaxItems=5
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// Autoscaling maps to the CCE node pool autoscaling
	// (NodePoolNodeAutoscaling). Only honored when the NodePoolAutoscaling
	// feature gate is enabled (Alpha, off by default); otherwise scaling is
	// driven solely by CAPI MachinePool replicas (questionnaire Q3, FR-2.6).
	// +optional
	Autoscaling AutoscalingSpec `json:"autoscaling,omitempty"`

	// UpdateConfig controls how spec changes are rolled onto existing nodes
	// (CCE 同步节点池 UpgradeNodePool, the analogue of CAPA's UpdateConfig /
	// rolling update). Node attributes such as securityGroups, taints, labels
	// and OS only apply to newly created nodes, so the controller calls
	// UpgradeNodePool to synchronise them onto running nodes.
	// +optional
	UpdateConfig UpdateConfigSpec `json:"updateConfig,omitempty"`
}

// UpdateConfigSpec maps the CCE 同步节点池 (UpgradeNodePool) parameters.
type UpdateConfigSpec struct {
	// MaxUnavailable is the maximum number of nodes made unavailable per
	// rolling batch (official range [1,20]; default 1).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	// +optional
	MaxUnavailable int32 `json:"maxUnavailable,omitempty"`
}

// ExtensionScaleGroupSpec describes one CCE extension scale group — an
// additional flavor/AZ set in a multi-AZ node pool.
type ExtensionScaleGroupSpec struct {
	// Name of the extension scale group.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Flavor is the ECS flavor for this group.
	// +kubebuilder:validation:Required
	Flavor string `json:"flavor"`

	// AvailabilityZone for this group.
	// +kubebuilder:validation:Required
	AvailabilityZone string `json:"availabilityZone"`
}

// AutoscalingSpec maps the CCE node pool autoscaling configuration
// (NodePoolNodeAutoscaling: enable / minNodeCount / maxNodeCount).
type AutoscalingSpec struct {
	// Enable turns autoscaling on for the node pool.
	// +optional
	Enable bool `json:"enable,omitempty"`

	// MinNodeCount is the minimum node count when autoscaling is enabled.
	// +optional
	MinNodeCount int32 `json:"minNodeCount,omitempty"`

	// MaxNodeCount is the maximum node count when autoscaling is enabled.
	// +optional
	MaxNodeCount int32 `json:"maxNodeCount,omitempty"`
}

// CCEManagedMachinePoolStatus defines the observed state of CCEManagedMachinePool.
type CCEManagedMachinePoolStatus struct {
	// Ready indicates the node pool is ready.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Replicas is the actual number of nodes (consumed by CAPI MachinePool).
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// AvailableReplicas is the number of nodes in Active state.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`

	// NodePoolID is the CCE node pool UUID.
	// +optional
	NodePoolID string `json:"nodePoolID,omitempty"`

	// LastAppliedSecurityGroups records the security groups last synced to the
	// cloud node pool. Used to detect attribute drift so the controller only
	// issues an UpdateNodePool when needed (and never rescales the pool just
	// because an attribute changed — questionnaire Q3/Q5).
	// +optional
	LastAppliedSecurityGroups []string `json:"lastAppliedSecurityGroups,omitempty"`

	// LastAppliedAutoscaling records the autoscaling config last synced to the
	// cloud node pool (only meaningful when the NodePoolAutoscaling gate is
	// on). Mirrors spec.autoscaling for drift detection.
	// +optional
	LastAppliedAutoscaling AutoscalingSpec `json:"lastAppliedAutoscaling,omitempty"`

	// Conditions defines current service state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=ccemanagedmachinepools,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this CCEManagedMachinePool belongs"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas",description="Actual node count"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Node pool ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CCEManagedMachinePool is the Schema for the CCE node pools
// (InfrastructureMachinePool).
type CCEManagedMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CCEManagedMachinePoolSpec   `json:"spec,omitempty"`
	Status CCEManagedMachinePoolStatus `json:"status,omitempty"`
}

// GetConditions implements the v1beta2 conditions contract.
func (m *CCEManagedMachinePool) GetConditions() []metav1.Condition {
	return m.Status.Conditions
}

// SetConditions implements the v1beta2 conditions contract.
func (m *CCEManagedMachinePool) SetConditions(conditions []metav1.Condition) {
	m.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// CCEManagedMachinePoolList contains a list of CCEManagedMachinePool.
type CCEManagedMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEManagedMachinePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CCEManagedMachinePool{}, &CCEManagedMachinePoolList{})
}
