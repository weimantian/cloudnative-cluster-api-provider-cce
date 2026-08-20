/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

// CCEClusterSpec defines the desired state of CCECluster.
// CCECluster is the thin "shell" (InfrastructureCluster contract object) that
// carries cluster-level network/region configuration. The control plane
// implementation lives in CCEManagedControlPlane (controlplane group).
type CCEClusterSpec struct {
	// Region is the Huawei Cloud region (e.g. cn-north-4). Required.
	// +kubebuilder:validation:Required
	Region string `json:"region"`

	// Network references the VPC/subnets the CCE cluster consumes.
	// CCE requires a VPC to exist before cluster creation.
	// +optional
	Network common.NetworkSpec `json:"network,omitempty"`
}

// CCEClusterStatus defines the observed state of CCECluster.
type CCEClusterStatus struct {
	// Ready indicates the cluster infrastructure (network validation) is
	// complete; consumed by CAPI core to flip Cluster.Status.InfrastructureReady.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Initialization reports infrastructure provisioning state. Provisioned
	// mirrors the CAPI InfrastructureCluster contract path
	// status.initialization.provisioned, which the CAPI Cluster controller
	// gates Cluster.Status.Initialization.InfrastructureProvisioned on.
	// +optional
	Initialization ClusterInitializationStatus `json:"initialization,omitempty"`

	// ClusterID is the CCE cluster UUID (backfilled by the control plane
	// controller).
	// +optional
	ClusterID string `json:"clusterID,omitempty"`

	// Conditions defines current service state of the CCECluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterInitializationStatus reports the initialization state of the
// infrastructure. Provisioned is the CAPI InfrastructureCluster contract
// field (status.initialization.provisioned).
type ClusterInitializationStatus struct {
	// Provisioned indicates the infrastructure is provisioned (CAPI contract).
	// +optional
	Provisioned bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=cceclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this CCECluster belongs"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="CCECluster is ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CCECluster is the Schema for the CCE clusters (InfrastructureCluster).
type CCECluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CCEClusterSpec   `json:"spec,omitempty"`
	Status CCEClusterStatus `json:"status,omitempty"`
}

// GetConditions implements the v1beta2 conditions contract.
func (c *CCECluster) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions implements the v1beta2 conditions contract.
func (c *CCECluster) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// CCEClusterList contains a list of CCECluster.
type CCEClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCECluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CCECluster{}, &CCEClusterList{})
}
