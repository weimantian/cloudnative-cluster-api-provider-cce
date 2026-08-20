/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CCEManagedControlPlaneTemplateSpec defines the desired state of
// CCEManagedControlPlaneTemplate. It wraps the CCEManagedControlPlaneSpec so
// the Cluster API topology controller can stamp a CCEManagedControlPlane from
// a ClusterClass (ControlPlaneTemplate contract: spec.template.spec).
type CCEManagedControlPlaneTemplateSpec struct {
	Template CCEManagedControlPlaneTemplateResource `json:"template"`
}

// CCEManagedControlPlaneTemplateResource describes the data needed to create a
// CCEManagedControlPlane from a template.
type CCEManagedControlPlaneTemplateResource struct {
	Spec CCEManagedControlPlaneSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ccemanagedcontrolplanetemplates,scope=Namespaced,categories=cluster-api,shortName=ccemcpt
// +kubebuilder:storageversion

// CCEManagedControlPlaneTemplate is the Schema for the
// CCEManagedControlPlaneTemplates API (ControlPlaneTemplate).
type CCEManagedControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CCEManagedControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CCEManagedControlPlaneTemplateList contains a list of
// CCEManagedControlPlaneTemplate.
type CCEManagedControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEManagedControlPlaneTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CCEManagedControlPlaneTemplate{}, &CCEManagedControlPlaneTemplateList{})
}
