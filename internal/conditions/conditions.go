/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package conditions defines the condition constants and small helpers used by
// the provider controllers. It wraps the Cluster API v1beta2 conditions API
// (sigs.k8s.io/cluster-api/util/conditions with []metav1.Condition).
package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiconditions "sigs.k8s.io/cluster-api/util/conditions"
)

// Setter is the v1beta2 conditions contract implemented by the provider CRDs.
type Setter = capiconditions.Setter

// CCECluster (InfraCluster) conditions.
const (
	// NetworkReadyCondition reports whether the referenced VPC/subnets are
	// validated (BYO mode) or the managed network is reconciled (managed mode).
	NetworkReadyCondition = "NetworkReady"
	// VpcReadyCondition reports successful reconciliation of the managed VPC.
	VpcReadyCondition = "VpcReady"
	// SubnetsReadyCondition reports successful reconciliation of managed
	// subnets.
	SubnetsReadyCondition = "SubnetsReady"
	// NatGatewaysReadyCondition reports successful reconciliation of the
	// managed NAT gateway + SNAT rules.
	NatGatewaysReadyCondition = "NatGatewaysReady"
	// SecurityGroupsReadyCondition reports successful reconciliation of the
	// managed node security group + its ingress/egress rules.
	SecurityGroupsReadyCondition = "SecurityGroupsReady"
)

// CCEManagedControlPlane (ControlPlane) conditions.
const (
	// CredentialsReadyCondition reports whether the CCE credentials resolved.
	CredentialsReadyCondition = "CredentialsReady"
	// CCEClusterReadyCondition reports whether the CCE cluster is Available.
	CCEClusterReadyCondition = "CCEClusterReady"
	// KubeconfigReadyCondition reports whether the kubeconfig Secret exists.
	KubeconfigReadyCondition = "KubeconfigReady"
	// AddonsConfiguredCondition reports whether the declared CCE addons are
	// installed/upgraded/removed as specified.
	AddonsConfiguredCondition = "AddonsConfigured"
	// PodIdentityAssociationsConfiguredCondition reports whether the declared
	// pod-identity associations are created/removed as specified.
	PodIdentityAssociationsConfiguredCondition = "PodIdentityAssociationsConfigured"
	// LoggingConfiguredCondition reports whether the declared control-plane log
	// collection config is applied.
	LoggingConfiguredCondition = "LoggingConfigured"
	// AccessPoliciesConfiguredCondition reports whether the declared CCE access
	// policies are created/updated/removed as specified.
	AccessPoliciesConfiguredCondition = "AccessPoliciesConfigured"
	// UpgradeReadyCondition reports the cluster upgrade state (FR-1.7,
	// questionnaire Q11). True when spec.version matches the running version.
	UpgradeReadyCondition = "UpgradeReady"
)

// CCEManagedMachinePool (InfraMachinePool) conditions.
const (
	// NodePoolReadyCondition reports whether the CCE node pool is ready.
	NodePoolReadyCondition = "NodePoolReady"
	// NodePoolScalingCondition reports an in-flight scaling operation.
	NodePoolScalingCondition = "NodePoolScaling"
)

// Reasons (shared, used when no condition-specific reason applies).
const (
	ReconciliationInProgressReason        = "ReconciliationInProgress"
	ReconciliationFailedReason            = "ReconciliationFailed"
	WaitingForClusterInfrastructureReason = "WaitingForClusterInfrastructure"
	WaitingForControlPlaneReason          = "WaitingForControlPlane"
	// UpgradeNotOfferedReason reports that the platform currently offers no
	// upgrade target from the running version (questionnaire Q11, verified
	// live: ShowClusterUpgradeInfo returns an empty target list).
	UpgradeNotOfferedReason = "UpgradeNotOffered"
	// UpgradeInProgressReason reports an in-flight upgrade task.
	UpgradeInProgressReason = "UpgradeInProgress"
	// UpgradeTargetUnavailableReason reports that the requested target version
	// is not among the platform-offered upgrade targets.
	UpgradeTargetUnavailableReason = "UpgradeTargetUnavailable"
)

// Per-condition reason constants. A dedicated reason exists only for the
// failure modes that actually need disambiguation; other failures reuse the
// shared Reconciliation{Failed,InProgress} reasons above, with the detail in
// the free-form Message field.

// Network condition reasons (CCECluster / VpcReady / SubnetsReady /
// NatGatewaysReady).
const (
	NetworkValidationFailedReason     = "NetworkValidationFailed"     // CIDR/overlap/subnet-ownership check failed
	NetworkReconciliationFailedReason = "NetworkReconciliationFailed" // managed VPC/subnet/NAT create/update failed
)

// Credentials condition reasons.
const (
	CredentialsResolutionFailedReason = "CredentialsResolutionFailed" // identityRef/secret/Secret not found
	AgencyCreationFailedReason        = "AgencyCreationFailed"        // EnsureAgency (List/Create trust agency) failed
)

// CCEClusterReady condition reasons.
const (
	CCEClusterNotFoundReason     = "CCEClusterNotFound" // out-of-band delete, recreate path
	CCEClusterNameMismatchReason = "InvalidClusterName" // spec.clusterName != owning Cluster name (GC would orphan-delete)
)

// KubeconfigReady condition reasons.
const (
	KubeconfigGenerationFailedReason = "KubeconfigGenerationFailed" // GetClusterKubeconfig API failed
)

// AddonsConfigured condition reasons.
const (
	AddonInstallFailedReason = "AddonInstallFailed" // CreateAddonInstance failed
)

// PodIdentityAssociationsConfigured condition reasons.
const (
	PodIdentityCreationFailedReason = "PodIdentityCreationFailed" // CreatePodIdentityAssociation failed
)

// LoggingConfigured condition reasons.
const (
	LogConfigUpdateFailedReason = "LogConfigUpdateFailed" // UpdateClusterLogConfig failed
)

// AccessPoliciesConfigured condition reasons.
const (
	AccessPolicyCreateFailedReason = "AccessPolicyCreateFailed" // CreateAccessPolicy failed
)

// NodePoolReady / NodePoolScaling condition reasons.
const (
	NodePoolCreationFailedReason = "NodePoolCreationFailed" // CreateNodePool failed
	NodePoolScaleFailedReason    = "NodePoolScaleFailed"    // ScaleNodePool failed
)

// MarkTrue sets a condition to True.
func MarkTrue(obj Setter, conditionType, reason, message string) {
	capiconditions.Set(obj, metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

// MarkFalse sets a condition to False. Note: the CAPI v1beta2 conditions
// contract uses standard metav1.Condition which carries no severity field;
// failures are conveyed via Reason/Message.
func MarkFalse(obj Setter, conditionType, reason, message string) {
	capiconditions.Set(obj, metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}
