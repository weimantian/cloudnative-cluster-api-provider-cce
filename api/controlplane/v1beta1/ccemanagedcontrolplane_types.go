/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// CCEManagedControlPlaneSpec defines the desired state of
// CCEManagedControlPlane. It maps to the CCE CreateCluster API (ClusterSpec):
// category / flavor / version / containerNetwork / serviceNetwork /
// customSan / publicAccess / agencyName / billingMode.
type CCEManagedControlPlaneSpec struct {
	// ClusterName of the owning CCE cluster.
	// +kubebuilder:validation:Required
	ClusterName string `json:"clusterName"`

	// Version of Kubernetes, e.g. "v1.30.0". Empty means CCE latest.
	// +optional
	Version string `json:"version,omitempty"`

	// Category of the CCE cluster: CCE (Standard) or Turbo.
	// +kubebuilder:validation:Enum=CCE;Turbo
	// +kubebuilder:default=Turbo
	// +optional
	Category string `json:"category,omitempty"`

	// Flavor of the cluster (official enum e.g. cce.s1.small ... cce.s2.xlarge).
	// +optional
	Flavor string `json:"flavor,omitempty"`

	// ContainerNetwork of the cluster.
	// +optional
	ContainerNetwork ContainerNetworkSpec `json:"containerNetwork,omitempty"`

	// ServiceNetwork of the cluster.
	// +optional
	ServiceNetwork ServiceNetworkSpec `json:"serviceNetwork,omitempty"`

	// CustomSan entries for the API server certificate.
	// +optional
	CustomSan []string `json:"customSan,omitempty"`

	// EndpointAccess controls public API server access.
	// +optional
	EndpointAccess EndpointAccessSpec `json:"endpointAccess,omitempty"`

	// AgencyName used by the cluster (1.27+; empty uses the system agency).
	// +optional
	AgencyName string `json:"agencyName,omitempty"`

	// IdentityRef references a CCECluster*Identity (Controller/Static/Role).
	// Empty means the controller default identity (CLOUD_SDK_AK/SK env).
	// +optional
	IdentityRef *corev1.ObjectReference `json:"identityRef,omitempty"`

	// Billing controls billing mode: 0=on-demand, 1=subscription.
	// +optional
	Billing BillingSpec `json:"billing,omitempty"`

	// Addons are the CCE addon instances to manage (declarative set; the
	// controller installs missing ones, upgrades version drift, and removes
	// those no longer listed — mirrors CAPA EKS addons).
	// +optional
	Addons []AddonSpec `json:"addons,omitempty"`

	// PodIdentityAssociations bind Kubernetes ServiceAccounts to Huawei Cloud
	// agencies (the CCE equivalent of EKS Pod Identity). Declarative set:
	// create missing, delete removed.
	// +optional
	PodIdentityAssociations []PodIdentityAssociationSpec `json:"podIdentityAssociations,omitempty"`

	// Logging configures control-plane log collection (mirrors CAPA EKS
	// Logging). Maps to CCE UpdateClusterLogConfig / ShowClusterConfig.
	// +optional
	Logging *ControlPlaneLoggingSpec `json:"logging,omitempty"`

	// AccessPolicies declare CCE access policies (the CCE equivalent of EKS
	// access entries): which IAM principal (user/group/agency) may access the
	// cluster with which role, scoped to which namespaces. Declarative set:
	// create missing, update drift, remove those no longer listed.
	// +optional
	AccessPolicies []AccessPolicySpec `json:"accessPolicies,omitempty"`
}

// ControlPlaneLoggingSpec maps the CCE ClusterLogConfig (ttl_in_days +
// log_configs).
type ControlPlaneLoggingSpec struct {
	// TTLInDays is the log retention in days (official range 0-30).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=30
	// +optional
	TTLInDays int32 `json:"ttlInDays,omitempty"`

	// Logs lists the components to collect.
	// +optional
	Logs []ControlPlaneLogSpec `json:"logs,omitempty"`
}

// ControlPlaneLogSpec declares one log collection item.
type ControlPlaneLogSpec struct {
	// Name is the log type: kube-apiserver, kube-controller-manager,
	// kube-scheduler, audit, or a system addon name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Type is the component type: control, audit, or system-addon.
	// +kubebuilder:validation:Enum=control;audit;system-addon
	// +optional
	Type string `json:"type,omitempty"`

	// Enable turns collection on/off for this item.
	// +optional
	Enable bool `json:"enable,omitempty"`
}

// AccessPolicySpec declares one CCE access policy (maps to the CCE
// AccessPolicy API: principal + policyType + accessScope.namespaces). The
// controller scopes it to the owning cluster (clusters=[clusterID]).
type AccessPolicySpec struct {
	// Name of the access policy. Unique within the account; lowercase start,
	// [a-z0-9.-], max 56 chars.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=56
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9.-]*$`
	Name string `json:"name"`

	// PolicyType is the permission level: CCEClusterAdminPolicy (cluster
	// admin), CCEAdminPolicy (ops), CCEEditPolicy (developer),
	// CCEViewPolicy (read-only).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=CCEClusterAdminPolicy;CCEAdminPolicy;CCEEditPolicy;CCEViewPolicy
	PolicyType string `json:"policyType"`

	// PrincipalType is the IAM principal kind: user, group or agency.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=user;group;agency
	PrincipalType string `json:"principalType"`

	// PrincipalIds are the IAM user/group/agency IDs the policy applies to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	PrincipalIds []string `json:"principalIds"`

	// Namespaces the policy applies to (["*"] = all namespaces). Defaults
	// to ["*"] when empty.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// PodIdentityAssociationSpec declares a ServiceAccount -> agency binding.
type PodIdentityAssociationSpec struct {
	// Namespace of the ServiceAccount (immutable per association).
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// ServiceAccount name (one association per ServiceAccount).
	// +kubebuilder:validation:Required
	ServiceAccount string `json:"serviceAccount"`

	// AgencyName is the Huawei Cloud agency (委托) to bind.
	// +kubebuilder:validation:Required
	AgencyName string `json:"agencyName"`
}

// AddonSpec declares a CCE addon to install.
type AddonSpec struct {
	// Name is the addon template name (e.g. "coredns", "metrics-server").
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Version is the addon template version (e.g. "1.0.0"); empty means the
	// latest supported by the cluster.
	// +optional
	Version string `json:"version,omitempty"`
}

// ContainerNetworkSpec mirrors the CCE ContainerNetwork model.
type ContainerNetworkSpec struct {
	// Mode: overlay_l2 | vpc-router | eni (eni implies Turbo).
	// +kubebuilder:validation:Enum=overlay_l2;vpc-router;eni
	// +kubebuilder:default=eni
	// +optional
	Mode string `json:"mode,omitempty"`

	// CIDR of the container network (immutable after creation for tunnel mode).
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// ENISubnets for eni mode (Turbo).
	// +optional
	ENISubnets []string `json:"eniSubnets,omitempty"`
}

// ServiceNetworkSpec mirrors the CCE service network (default 10.247.0.0/16).
type ServiceNetworkSpec struct {
	// CIDR of the service network.
	// +optional
	CIDR string `json:"cidr,omitempty"`
}

// EndpointAccessSpec controls API server access.
type EndpointAccessSpec struct {
	// Public enables public API server access.
	// +optional
	Public bool `json:"public,omitempty"`

	// CIDRs is the public API server access whitelist (mapped to the CCE
	// PublicAccess.cidrs). Only sent when public access is enabled; empty
	// means the platform default ["0.0.0.0/0"]. CCE always exposes a
	// VPC-internal (private) endpoint regardless of this field.
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`
}

// BillingSpec controls cluster billing.
type BillingSpec struct {
	// Mode: 0=on-demand, 1=subscription.
	// +kubebuilder:validation:Enum=0;1
	// +kubebuilder:default=0
	// +optional
	Mode int32 `json:"mode,omitempty"`
}

// CCEManagedControlPlaneStatus defines the observed state of
// CCEManagedControlPlane.
type CCEManagedControlPlaneStatus struct {
	// Ready indicates the CCE control plane is available.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Initialized indicates the kubeconfig Secret has been generated.
	// +optional
	Initialized bool `json:"initialized,omitempty"`

	// ClusterID is the CCE cluster UUID.
	// +optional
	ClusterID string `json:"clusterID,omitempty"`

	// ControlPlaneEndpoint is the API server endpoint.
	// +optional
	ControlPlaneEndpoint *clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// KubeconfigSecretName of the generated kubeconfig Secret.
	// +optional
	KubeconfigSecretName string `json:"kubeconfigSecretName,omitempty"`

	// Version of the cluster as reported by CCE.
	// +optional
	Version string `json:"version,omitempty"`

	// UpgradeTaskID of the in-flight cluster upgrade (empty = no upgrade
	// running). Set when spec.version differs from the running version and an
	// upgrade workflow was started (FR-1.7, questionnaire Q11).
	// +optional
	UpgradeTaskID string `json:"upgradeTaskID,omitempty"`

	// Conditions defines current service state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=ccemanagedcontrolplanes,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this CCEManagedControlPlane belongs"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Control plane ready"
// +kubebuilder:printcolumn:name="Initialized",type="string",JSONPath=".status.initialized",description="kubeconfig generated"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CCEManagedControlPlane is the Schema for the CCE managed control plane
// (ControlPlane).
type CCEManagedControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CCEManagedControlPlaneSpec   `json:"spec,omitempty"`
	Status CCEManagedControlPlaneStatus `json:"status,omitempty"`
}

// GetConditions implements the v1beta2 conditions contract.
func (c *CCEManagedControlPlane) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions implements the v1beta2 conditions contract.
func (c *CCEManagedControlPlane) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// CCEManagedControlPlaneList contains a list of CCEManagedControlPlane.
type CCEManagedControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CCEManagedControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CCEManagedControlPlane{}, &CCEManagedControlPlaneList{})
}
