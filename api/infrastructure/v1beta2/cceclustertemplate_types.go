/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CCEClusterTemplateSpec defines the desired state of CCEClusterTemplate.
// It wraps the CCEClusterSpec so the Cluster API topology controller can
// stamp a CCECluster from a ClusterClass (InfrastructureClusterTemplate
// contract: spec.template.spec).
type CCEClusterTemplateSpec struct {
	Template CCEClusterTemplateResource `json:"template"`
}

// CCEClusterTemplateResource describes the data needed to create a CCECluster
// from a template.
type CCEClusterTemplateResource struct {
	Spec CCEClusterSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:path=cceclustertemplates,scope=Namespaced,categories=cluster-api,shortName=ccect

// CCEClusterTemplate is the Schema for the CCEClusterTemplates API
// (InfrastructureClusterTemplate).
type CCEClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CCEClusterTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// CCEClusterTemplateList contains a list of CCEClusterTemplate.
type CCEClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CCEClusterTemplate{}, &CCEClusterTemplateList{})
}
