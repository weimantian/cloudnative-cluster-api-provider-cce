/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CCEManagedMachinePoolTemplateSpec defines the desired state of
// CCEManagedMachinePoolTemplate. It wraps the CCEManagedMachinePoolSpec so the
// Cluster API topology controller can stamp a CCEManagedMachinePool from a
// ClusterClass MachinePoolClass (InfrastructureMachinePoolTemplate contract:
// spec.template.spec).
type CCEManagedMachinePoolTemplateSpec struct {
	Template CCEManagedMachinePoolTemplateResource `json:"template"`
}

// CCEManagedMachinePoolTemplateResource describes the data needed to create a
// CCEManagedMachinePool from a template.
type CCEManagedMachinePoolTemplateResource struct {
	Spec CCEManagedMachinePoolSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:path=ccemanagedmachinepooltemplates,scope=Namespaced,categories=cluster-api,shortName=ccemmpt

// CCEManagedMachinePoolTemplate is the Schema for the
// CCEManagedMachinePoolTemplates API (InfrastructureMachinePoolTemplate).
type CCEManagedMachinePoolTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CCEManagedMachinePoolTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// CCEManagedMachinePoolTemplateList contains a list of
// CCEManagedMachinePoolTemplate.
type CCEManagedMachinePoolTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEManagedMachinePoolTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CCEManagedMachinePoolTemplate{}, &CCEManagedMachinePoolTemplateList{})
}
