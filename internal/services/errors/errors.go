/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package errors provides Huawei Cloud SDK error classification used by the
// services layer. The pattern follows CAPHW pkg/errors (BaseErrorHandler)
// which extracts StatusCode/ErrorCode from *sdkerr.ServiceResponseError.
package errors

import (
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	"github.com/pkg/errors"
)

// Known CCE error codes observed from the official SDK/docs. The complete
// catalog is a verification item (questionnaire Q14) — extend as confirmed.
const (
	// ErrCodeClusterNotFound is returned by ShowCluster for a missing cluster
	// (to be confirmed against the real catalog, questionnaire Q14).
	ErrCodeClusterNotFound = "CCE.01410001"
)

// IsNotFound reports whether err is a "resource not found" SDK error.
func IsNotFound(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 404 || sdkErr.ErrorCode == ErrCodeClusterNotFound
	}
	return false
}

// IsConflict reports whether err is an "already exists" SDK error.
func IsConflict(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 409 || sdkErr.StatusCode == 400
	}
	return false
}

// IsThrottled reports whether err is an API rate-limit error (429), used to
// drive exponential backoff (questionnaire Q14).
func IsThrottled(err error) bool {
	var sdkErr *sdkerr.ServiceResponseError
	if errors.As(err, &sdkErr) {
		return sdkErr.StatusCode == 429
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
