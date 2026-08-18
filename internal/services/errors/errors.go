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
	// ErrCodeInsufficientSecurityGroupQuota: 400 Insufficient security group quota.
	ErrCodeInsufficientSecurityGroupQuota = "CCE.01400011"
	// ErrCodeInsufficientVolumeQuota: 400 Insufficient volume quota.
	ErrCodeInsufficientVolumeQuota = "CCE.01400013"
	// ErrCodeInsufficientVPCQuota: 400 Insufficient VPC quota.
	ErrCodeInsufficientVPCQuota = "CCE.01400020"
	// ErrCodeInsufficientSubENIQuota: 400 unsupported flavor with insufficient sub-ENI quota.
	ErrCodeInsufficientSubENIQuota = "CCE.01400025"
	// ErrCodeResourceNotFound: 404 Resource not found (cluster/node pool/node).
	ErrCodeResourceNotFound = "CCE.01404001"
	// ErrCodeNodePoolStateNotAllowDelete: 403 current node pool status does not allow deletion.
	ErrCodeNodePoolStateNotAllowDelete = "CCE.01403003"
	// ErrCodeClusterStateNotAllowNodePoolDelete: 403 cluster status does not allow node pool deletion.
	ErrCodeClusterStateNotAllowNodePoolDelete = "CCE.01403009"
	// ErrCodeResourceLocked: 429 Resource locked by other requests.
	ErrCodeResourceLocked = "CCE.01429002"
	// ErrCodeConcurrencyLimit: 429 the concurrency limit of tasks has been reached.
	ErrCodeConcurrencyLimit = "CCE.01429003"
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
func IsConflict(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 409
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

// IsQuotaExceeded reports whether err is a quota-exceeded error
// (CCE.01400007/08/09/10/11/12/13/19/20/25).
func IsQuotaExceeded(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		switch sdkErr.ErrorCode {
		case ErrCodeInsufficientClusterQuota,
			ErrCodeInsufficientServerQuota,
			ErrCodeInsufficientSecurityGroupQuota,
			ErrCodeInsufficientVolumeQuota,
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
