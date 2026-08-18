/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"time"

	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// requeueAfterForError maps classified CCE API errors to a requeue delay
// (official error codes — questionnaire Q14):
//   - throttled (429 / CCE.01429002/003, APIGW.0308): back off briefly;
//   - quota exceeded (CCE.01400007...): wait longer before retrying;
//   - permission/auth (401/403): long delay — likely needs user action;
//   - otherwise the default reconcile interval.
func requeueAfterForError(err error) time.Duration {
	switch {
	case clouderrors.IsThrottled(err):
		return time.Minute
	case clouderrors.IsQuotaExceeded(err):
		return 5 * time.Minute
	case clouderrors.IsPermissionDenied(err):
		return 30 * time.Minute
	default:
		return defaultRequeue
	}
}
