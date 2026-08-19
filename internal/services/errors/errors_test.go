/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package errors

import (
	"testing"

	sdkerr "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	"github.com/pkg/errors"
)

func sdk(code string, status int) error {
	return errors.Wrap(&sdkerr.ServiceResponseError{
		StatusCode:   status,
		ErrorCode:    code,
		ErrorMessage: "x",
	}, "wrapped")
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(sdk(ErrCodeResourceNotFound, 404)) {
		t.Error("expected 404 CCE.01404001 to be NotFound")
	}
	if IsNotFound(sdk("", 400)) {
		t.Error("400 must not be NotFound")
	}
}

func TestIsConflict(t *testing.T) {
	cases := []string{
		ErrCodeResourceAlreadyExists, ErrCodeResourceVersionExpired,
		ErrCodeOperationConflictCreatingNode, ErrCodeOperationConflictDeletingCluster,
		ErrCodeContainerNetworkCIDRConflict, ErrCodeContainerCIDRConflictCM,
	}
	for _, c := range cases {
		if !IsConflict(sdk(c, 400)) {
			t.Errorf("expected %s to be Conflict", c)
		}
	}
	if !IsConflict(sdk("", 409)) {
		t.Error("409 status must be Conflict")
	}
	if IsConflict(sdk("", 400)) {
		t.Error("plain 400 must not be Conflict")
	}
}

func TestIsThrottled(t *testing.T) {
	if !IsThrottled(sdk(ErrCodeResourceLocked, 429)) || !IsThrottled(sdk(ErrCodeConcurrencyLimit, 429)) {
		t.Error("expected 429 codes to be Throttled")
	}
	if !IsThrottled(sdk("APIGW.0308", 429)) {
		t.Error("429 status fallback must be Throttled")
	}
	if IsThrottled(sdk("", 400)) {
		t.Error("400 must not be Throttled")
	}
}

func TestIsQuotaExceeded(t *testing.T) {
	cases := []string{
		ErrCodeInsufficientClusterQuota, ErrCodeInsufficientServerQuota,
		ErrCodeInsufficientCPUQuota, ErrCodeInsufficientMemoryQuota,
		ErrCodeInsufficientSecurityGroupQuota, ErrCodeInsufficientEIPQuota,
		ErrCodeInsufficientVolumeQuota, ErrCodeInsufficientResourceTenantQuota,
		ErrCodeInsufficientVPCQuota, ErrCodeInsufficientSubENIQuota,
	}
	for _, c := range cases {
		if !IsQuotaExceeded(sdk(c, 400)) {
			t.Errorf("expected %s to be QuotaExceeded", c)
		}
	}
	if IsQuotaExceeded(sdk("", 400)) {
		t.Error("plain 400 must not be QuotaExceeded")
	}
}

func TestIsPermissionDenied(t *testing.T) {
	// Real permission/auth codes.
	for _, c := range []string{ErrCodeAuthenticationFailure, ErrCodePermissionDenied, ErrCodeAccountRestricted, ErrCodeNoAgencyPermission} {
		if !IsPermissionDenied(sdk(c, 403)) {
			t.Errorf("expected %s to be PermissionDenied", c)
		}
	}
	if !IsPermissionDenied(sdk("", 401)) {
		t.Error("401 must be PermissionDenied")
	}
	// Transient state-conflict 403 codes must NOT be permission errors.
	for _, c := range []string{ErrCodeNodePoolStateNotAllowDelete, ErrCodeClusterStateNotAllowNodePoolDelete} {
		if IsPermissionDenied(sdk(c, 403)) {
			t.Errorf("%s is a transient state conflict, must not be PermissionDenied", c)
		}
	}
	if IsPermissionDenied(sdk("", 403)) {
		t.Error("plain 403 without a permission code must not be PermissionDenied")
	}
}

func TestServiceResponseError(t *testing.T) {
	code, msg := ServiceResponseError(sdk(ErrCodeResourceNotFound, 404))
	if code != ErrCodeResourceNotFound {
		t.Errorf("expected code %s, got %s", ErrCodeResourceNotFound, code)
	}
	if msg != "x" {
		t.Errorf("expected message x, got %s", msg)
	}
	if c, m := ServiceResponseError(errors.New("plain")); c != "" || m != "" {
		t.Errorf("expected empty for non-SDK error, got %q %q", c, m)
	}
}
