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
	// VpcReadyCondition reports successful reconciliation of the managed VPC
	// (mirrors CAPA VpcReadyCondition).
	VpcReadyCondition = "VpcReady"
	// SubnetsReadyCondition reports successful reconciliation of managed
	// subnets (mirrors CAPA SubnetsReadyCondition).
	SubnetsReadyCondition = "SubnetsReady"
	// NatGatewaysReadyCondition reports successful reconciliation of the
	// managed NAT gateway + SNAT rules (mirrors CAPA NatGatewaysReadyCondition).
	NatGatewaysReadyCondition = "NatGatewaysReady"
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
	// collection config is applied (mirrors CAPA EKS Logging).
	LoggingConfiguredCondition = "LoggingConfigured"
	// AccessPoliciesConfiguredCondition reports whether the declared CCE access
	// policies are created/updated/removed as specified (mirrors EKS access
	// entries).
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
	WaitingForKubeconfigReason            = "WaitingForKubeconfig"
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

// Per-condition reason constants. Mirrors the CAPA v2.13.0 pattern of one
// dedicated reason per condition so downstream tooling (kubectl describe,
// status dashboards) can disambiguate failure modes without parsing the
// free-form Message field. The shared Reconciliation{Failed,InProgress}
// reasons remain valid for unexpected failures that fall outside these
// categories.

// Network condition reasons (CCECluster / VpcReady / SubnetsReady /
// NatGatewaysReady).
const (
	NetworkValidationFailedReason     = "NetworkValidationFailed"     // CIDR/overlap/subnet-ownership check failed
	NetworkReconciliationFailedReason = "NetworkReconciliationFailed"  // managed VPC/subnet/NAT create/update failed
)

// Credentials condition reasons.
const (
	CredentialsResolutionFailedReason = "CredentialsResolutionFailed" // identityRef/secret/Secret not found
	CredentialsInvalidReason          = "CredentialsInvalid"          // AK/SK rejected by the cloud
	AgencyCreationFailedReason        = "AgencyCreationFailed"        // EnsureAgency (List/Create trust agency) failed
)

// CCEClusterReady condition reasons.
const (
	CCEClusterNotFoundReason = "CCEClusterNotFound" // out-of-band delete, recreate path
	CCEClusterCreatingReason = "CCEClusterCreating" // cluster is being created
)

// KubeconfigReady condition reasons.
const (
	KubeconfigGenerationFailedReason = "KubeconfigGenerationFailed" // GetClusterKubeconfig API failed
)

// AddonsConfigured condition reasons.
const (
	AddonInstallFailedReason   = "AddonInstallFailed"   // CreateAddonInstance failed
	AddonUpgradeFailedReason   = "AddonUpgradeFailed"   // UpdateAddonInstance failed (version drift)
	AddonDeleteFailedReason    = "AddonDeleteFailed"    // DeleteAddonInstance failed (stale addon not removed)
)

// PodIdentityAssociationsConfigured condition reasons.
const (
	PodIdentityCreationFailedReason = "PodIdentityCreationFailed" // CreatePodIdentityAssociation failed
	PodIdentityDeletionFailedReason = "PodIdentityDeletionFailed" // DeletePodIdentityAssociation failed
)

// LoggingConfigured condition reasons.
const (
	LogConfigUpdateFailedReason = "LogConfigUpdateFailed" // UpdateClusterLogConfig failed
)

// AccessPoliciesConfigured condition reasons.
const (
	AccessPolicyCreateFailedReason = "AccessPolicyCreateFailed" // CreateAccessPolicy failed
	AccessPolicyUpdateFailedReason = "AccessPolicyUpdateFailed" // UpdateAccessPolicy (drift) failed
	AccessPolicyDeleteFailedReason = "AccessPolicyDeleteFailed" // DeleteAccessPolicy (stale) failed
)

// NodePoolReady / NodePoolScaling condition reasons.
const (
	NodePoolCreationFailedReason = "NodePoolCreationFailed"  // CreateNodePool failed
	NodePoolUpdateFailedReason   = "NodePoolUpdateFailed"    // UpdateNodePool (attribute drift) failed
	NodePoolScaleFailedReason    = "NodePoolScaleFailed"     // ScaleNodePool failed
	NodePoolDeleteFailedReason   = "NodePoolDeleteFailed"    // DeleteNodePool failed
	NodePoolReplicasExternallyManagedReason = "ReplicasExternallyManaged" // external autoscaler owns replicas
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
