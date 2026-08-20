/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AllowedNamespaces selects the namespaces a cluster identity can be used
// from. An empty list + empty selector means no namespace is allowed (nil
// pointer means any namespace — the CAPA contract).
type AllowedNamespaces struct {
	// NamespaceList is the explicit list of allowed namespaces. An nil or
	// empty list, combined with an empty selector, means no namespace is
	// allowed.
	// +optional
	// +nullable
	NamespaceList []string `json:"namespaceList,omitempty"`

	// Selector is a label selector over namespaces.
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`
}

// CCEClusterControllerIdentityName is the name of the default controller
// identity singleton (mirrors CAPA AWSClusterControllerIdentityName).
const CCEClusterControllerIdentityName = "default"

// CCEClusterControllerIdentitySpec defines the controller's own credentials
// (CLOUD_SDK_AK/SK environment). A single instance named "default" is used
// when no identityRef is set on a control plane.
type CCEClusterControllerIdentitySpec struct {
	// AllowedNamespaces restricts which namespaces may use this identity.
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:path=cceclustercontrolleridentities,scope=Cluster,categories=cluster-api,shortName=cceci

// CCEClusterControllerIdentity is the controller's default credential
// identity (mirrors CAPA AWSClusterControllerIdentity).
type CCEClusterControllerIdentity struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CCEClusterControllerIdentitySpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// CCEClusterControllerIdentityList contains a list of controller identities.
type CCEClusterControllerIdentityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEClusterControllerIdentity `json:"items"`
}

// CCEClusterStaticIdentitySpec defines static AK/SK credentials referenced
// from a Secret.
type CCEClusterStaticIdentitySpec struct {
	// SecretRef is the name of a Secret (in the controller namespace)
	// containing keys accessKey and secretKey.
	// +kubebuilder:validation:Required
	SecretRef string `json:"secretRef"`

	// AllowedNamespaces restricts which namespaces may use this identity.
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:path=cceclusterstaticidentities,scope=Cluster,categories=cluster-api,shortName=ccesi

// CCEClusterStaticIdentity is a reference to a static AK/SK Secret (mirrors
// CAPA AWSClusterStaticIdentity).
type CCEClusterStaticIdentity struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CCEClusterStaticIdentitySpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// CCEClusterStaticIdentityList contains a list of static identities.
type CCEClusterStaticIdentityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEClusterStaticIdentity `json:"items"`
}

// CCEClusterRoleIdentitySpec defines a Huawei Cloud agency (委托) based
// identity — the CCE equivalent of CAPA's assumed-role identity. The agency
// must already exist and grant trust to the calling account.
type CCEClusterRoleIdentitySpec struct {
	// AgencyName is the Huawei Cloud agency (委托) to use.
	// +kubebuilder:validation:Required
	AgencyName string `json:"agencyName"`

	// AllowedNamespaces restricts which namespaces may use this identity.
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:path=cceclusterroleidentities,scope=Cluster,categories=cluster-api,shortName=cceri

// CCEClusterRoleIdentity is a Huawei Cloud agency identity (mirrors CAPA
// AWSClusterRoleIdentity, but with an agency instead of an AssumeRole ARN).
type CCEClusterRoleIdentity struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CCEClusterRoleIdentitySpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// CCEClusterRoleIdentityList contains a list of role identities.
type CCEClusterRoleIdentityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEClusterRoleIdentity `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&CCEClusterControllerIdentity{}, &CCEClusterControllerIdentityList{},
		&CCEClusterStaticIdentity{}, &CCEClusterStaticIdentityList{},
		&CCEClusterRoleIdentity{}, &CCEClusterRoleIdentityList{},
	)
}
