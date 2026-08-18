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
	// "Huawei Cloud EulerOS 2.0".
	// +optional
	OS string `json:"os,omitempty"`

	// RootVolume of the nodes (nodeTemplate.rootVolume).
	// +optional
	RootVolume common.NodeVolume `json:"rootVolume,omitempty"`

	// DataVolumes of the nodes (nodeTemplate.dataVolumes).
	// +optional
	DataVolumes []common.NodeVolume `json:"dataVolumes,omitempty"`

	// SSHKey used to access the nodes (nodeTemplate.sshKey).
	// +optional
	SSHKey string `json:"sshKey,omitempty"`

	// AvailabilityZone of the nodes.
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

	// AdditionalTags attached to the node pool for ownership identification.
	// +optional
	AdditionalTags common.Tags `json:"additionalTags,omitempty"`
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

	// FailureReason is a short reason for failure.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage is a human-readable failure description.
	// +optional
	FailureMessage string `json:"failureMessage,omitempty"`

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
