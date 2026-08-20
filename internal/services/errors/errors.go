/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package errors provides Huawei Cloud SDK error classification used by the
// services layer. Error codes below are taken from the official CCE API error
// code reference (support.huaweicloud.com/api-cce/ErrorCode.html) and the
// official Go SDK (*sdkerr.ServiceResponseError).
package errors

import (
	"strings"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	"github.com/pkg/errors"
)

// Official CCE error codes (from the public ErrorCode reference; the catalog
// is large — extend as needed).
const (
	// ErrCodeInvalidRequest: 400 Invalid request.
	ErrCodeInvalidRequest = "CCE.01400001"
	// ErrCodeSubnetNotFoundInVPC: 400 Subnet not found in the VPC.
	ErrCodeSubnetNotFoundInVPC = "CCE.01400002"
	// ErrCodeContainerNetworkCIDRConflict: 400 Container network CIDR blocks conflict.
	ErrCodeContainerNetworkCIDRConflict = "CCE.01400005"
	// ErrCodeInsufficientClusterQuota: 400 Insufficient cluster quota.
	ErrCodeInsufficientClusterQuota = "CCE.01400007"
	// ErrCodeInsufficientServerQuota: 400 Insufficient server (ECS) quota.
	ErrCodeInsufficientServerQuota = "CCE.01400008"
	// ErrCodeInsufficientCPUQuota: 400 Insufficient CPU quota.
	ErrCodeInsufficientCPUQuota = "CCE.01400009"
	// ErrCodeInsufficientMemoryQuota: 400 Insufficient memory quota.
	ErrCodeInsufficientMemoryQuota = "CCE.01400010"
	// ErrCodeInsufficientSecurityGroupQuota: 400 Insufficient security group quota.
	ErrCodeInsufficientSecurityGroupQuota = "CCE.01400011"
	// ErrCodeInsufficientEIPQuota: 400 Insufficient EIP quota.
	ErrCodeInsufficientEIPQuota = "CCE.01400012"
	// ErrCodeInsufficientVolumeQuota: 400 Insufficient volume quota.
	ErrCodeInsufficientVolumeQuota = "CCE.01400013"
	// ErrCodeInsufficientResourceTenantQuota: 400 Insufficient resource tenant quota.
	ErrCodeInsufficientResourceTenantQuota = "CCE.01400019"
	// ErrCodeInsufficientVPCQuota: 400 Insufficient VPC quota.
	ErrCodeInsufficientVPCQuota = "CCE.01400020"
	// ErrCodeInsufficientSubENIQuota: 400 unsupported flavor with insufficient sub-ENI quota.
	ErrCodeInsufficientSubENIQuota = "CCE.01400025"
	// ErrCodeResourceNotFound: 404 Resource not found (cluster/node pool/node).
	ErrCodeResourceNotFound = "CCE.01404001"
	// ErrCodeAuthenticationFailure: 401 authentication failed.
	ErrCodeAuthenticationFailure = "CCE.01401001"
	// ErrCodePermissionDenied: 403 access denied / insufficient permission.
	ErrCodePermissionDenied = "CCE.01403001"
	// ErrCodeAccountRestricted: 403 account restricted.
	ErrCodeAccountRestricted = "CCE.01403002"
	// ErrCodeNoAgencyPermission: 403 no permission to create/authorize agencies.
	ErrCodeNoAgencyPermission = "CCE.01403008"
	// ErrCodeResourceAlreadyExists: 409 the resource already exists.
	ErrCodeResourceAlreadyExists = "CCE.01409001"
	// ErrCodeResourceVersionExpired: 409 the resource version is expired.
	ErrCodeResourceVersionExpired = "CCE.01409002"
	// ErrCodeNodePoolStateNotAllowDelete: 403 current node pool status does not allow deletion.
	ErrCodeNodePoolStateNotAllowDelete = "CCE.01403003"
	// ErrCodeClusterStateNotAllowNodePoolDelete: 403 cluster status does not allow node pool deletion.
	ErrCodeClusterStateNotAllowNodePoolDelete = "CCE.01403009"
	// ErrCodeResourceLocked: 429 Resource locked by other requests.
	ErrCodeResourceLocked = "CCE.01429002"
	// ErrCodeConcurrencyLimit: 429 the concurrency limit of tasks has been reached.
	ErrCodeConcurrencyLimit = "CCE.01429003"
	// ErrCodeOperationConflictCreatingNode: 400 operation conflict — cluster
	// is scaling, cannot create a node (ErrorCodes.txt).
	ErrCodeOperationConflictCreatingNode = "CCE.01400023"
	// ErrCodeOperationConflictDeletingCluster: 400 operation conflict — cannot
	// delete the cluster while a node is being created.
	ErrCodeOperationConflictDeletingCluster = "CCE.01400024"
	// ErrCodeContainerCIDRConflictCM: 400 container network CIDR conflict
	// (CCE_CM.0410, observed live; the CCE_CM.* family is not listed in the
	// official error-code table but is returned by the platform).
	ErrCodeContainerCIDRConflictCM = "CCE_CM.0410"
)

// IsNotFound reports whether err is a "resource not found" SDK error.
func IsNotFound(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 404 || sdkErr.ErrorCode == ErrCodeResourceNotFound
	}
	return false
}

// IsConflict reports whether err is an "already exists" / invalid-state error.
// Includes the official container-network CIDR conflict code (CCE.01400005,
// ErrorCodes.txt) and the live-observed CCE_CM.0410 (same meaning, returned
// by the platform as 400; not listed in the official error-code table).
func IsConflict(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 409 ||
			sdkErr.ErrorCode == ErrCodeResourceAlreadyExists ||
			sdkErr.ErrorCode == ErrCodeResourceVersionExpired ||
			sdkErr.ErrorCode == ErrCodeOperationConflictCreatingNode ||
			sdkErr.ErrorCode == ErrCodeOperationConflictDeletingCluster ||
			sdkErr.ErrorCode == ErrCodeContainerNetworkCIDRConflict ||
			sdkErr.ErrorCode == ErrCodeContainerCIDRConflictCM
	}
	return false
}

// IsPermissionDenied reports whether err is an authentication/permission error
// (401 CCE.01401001, 403 CCE.01403001/01403002/01403008). The 403 state-conflict
// codes (CCE.01403003~06/09 — "wait and retry") are deliberately NOT matched
// here: they are transient conditions, not permission failures, so they must
// not be parked on the long 30-minute backoff.
func IsPermissionDenied(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 401 ||
			sdkErr.ErrorCode == ErrCodeAuthenticationFailure ||
			sdkErr.ErrorCode == ErrCodePermissionDenied ||
			sdkErr.ErrorCode == ErrCodeAccountRestricted ||
			sdkErr.ErrorCode == ErrCodeNoAgencyPermission
	}
	return false
}

// IsThrottled reports whether err is an API rate-limit error (HTTP 429, codes
// CCE.01429002/01429003); drives exponential backoff.
func IsThrottled(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 429 ||
			sdkErr.ErrorCode == ErrCodeResourceLocked ||
			sdkErr.ErrorCode == ErrCodeConcurrencyLimit
	}
	return false
}

// IsQuotaExceeded reports whether err is a quota-exceeded error. Official
// quota codes (ErrorCodes.txt): cluster/server/security-group/volume/vpc/
// subENI/CPU/memory/EIP/resource-tenant.
func IsQuotaExceeded(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		switch sdkErr.ErrorCode {
		case ErrCodeInsufficientClusterQuota,
			ErrCodeInsufficientServerQuota,
			ErrCodeInsufficientCPUQuota,
			ErrCodeInsufficientMemoryQuota,
			ErrCodeInsufficientSecurityGroupQuota,
			ErrCodeInsufficientEIPQuota,
			ErrCodeInsufficientVolumeQuota,
			ErrCodeInsufficientResourceTenantQuota,
			ErrCodeInsufficientVPCQuota,
			ErrCodeInsufficientSubENIQuota:
			return true
		}
	}
	return false
}

// ServiceResponseError returns the SDK error code and message, or ("", "") for
// non-SDK errors.
func ServiceResponseError(err error) (code string, message string) {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.ErrorCode, sdkErr.ErrorMessage
	}
	return "", ""
}

// IsScaleNoOp reports whether err is the CCE "No scale task needed with desired
// node count N" rejection (CCE_CM.0004). The platform returns this when the
// pool is already at the requested count — e.g. right after creation with
// initialNodeCount == desired, or when a transient 0 node count races a scale.
// It means the scale is already satisfied, not a real failure (verified live).
func IsScaleNoOp(err error) bool {
	_, message := ServiceResponseError(err)
	return strings.Contains(message, "No scale task needed")
}
