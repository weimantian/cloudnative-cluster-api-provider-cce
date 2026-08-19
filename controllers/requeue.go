/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

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

// resultAfterError converts a classified CCE API error into a reconcile
// result. Rate-limit and quota errors are transient platform conditions
// (questionnaire Q14): return a delayed requeue with no error so the
// controller-runtime backoff does not override the delay and the error is not
// surfaced as a reconcile failure. All other errors pass through.
func resultAfterError(err error) (ctrl.Result, error) {
	if clouderrors.IsThrottled(err) || clouderrors.IsQuotaExceeded(err) {
		return ctrl.Result{RequeueAfter: requeueAfterForError(err)}, nil
	}
	return ctrl.Result{}, err
}
