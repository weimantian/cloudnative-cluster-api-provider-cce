/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// Exponential backoff base delays per classified error (the delay for the
// first failure) and the shared ceiling. Each consecutive throttled/quota
// failure doubles the delay up to backoffMax; a clean reconcile resets it.
const (
	// 429 (APIGW.0308) retries count towards Huawei Cloud's 1-minute write
	// window, so a 1-minute backoff always re-hits the window and the repeated
	// retries accumulate a longer platform penalty (429 stall lasted 20+ min).
	// 3 minutes > the window, letting it drain between retries.
	throttledBackoffBase = 3 * time.Minute
	quotaBackoffBase     = 5 * time.Minute
	permissionBackoff    = 30 * time.Minute // long, user-action; not exponential
	backoffMax           = 30 * time.Minute
)

// backoffState tracks consecutive failures for one reconcile key.
type backoffState struct {
	failures int
}

// backoffTracker is a process-local, mutex-guarded failure counter keyed by
// reconcile request. It backs exponential requeue delays: consecutive
// throttled/quota failures double the delay up to backoffMax, and a clean
// reconcile (no error, no requeue) resets the counter to zero.
type backoffTracker struct {
	mu    sync.Mutex
	state map[types.NamespacedName]*backoffState
}

func newBackoffTracker() *backoffTracker {
	return &backoffTracker{state: make(map[types.NamespacedName]*backoffState)}
}

// delay records one more failure for key and returns the exponential delay for
// that failure (base doubled per prior failure, capped at backoffMax).
func (b *backoffTracker) delay(key types.NamespacedName, base time.Duration) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.state[key]
	if st == nil {
		st = &backoffState{}
		b.state[key] = st
	}
	st.failures++
	delay := base
	for i := 1; i < st.failures; i++ {
		delay *= 2
		if delay >= backoffMax {
			return backoffMax
		}
	}
	return delay
}

// reset clears the failure counter for key.
func (b *backoffTracker) reset(key types.NamespacedName) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, key)
}

// errorBackoff is the shared tracker used by all controllers.
var errorBackoff = newBackoffTracker()

// requeueAfterForError maps classified CCE API errors to an exponential
// requeue delay (official error codes — questionnaire Q14):
//   - throttled (429 / APIGW.0308): back off briefly, doubling on repeat;
//   - quota exceeded: wait longer, doubling on repeat;
//   - permission/auth (401/403): fixed long delay — likely needs user action;
//   - otherwise the default reconcile interval.
func requeueAfterForError(key types.NamespacedName, err error) time.Duration {
	switch {
	case clouderrors.IsThrottled(err):
		return errorBackoff.delay(key, throttledBackoffBase)
	case clouderrors.IsQuotaExceeded(err):
		return errorBackoff.delay(key, quotaBackoffBase)
	case clouderrors.IsPermissionDenied(err):
		return permissionBackoff
	default:
		return defaultRequeue
	}
}

// resultAfterError converts a classified CCE API error into a reconcile
// result. Rate-limit and quota errors are transient platform conditions
// (questionnaire Q14): return an exponential delayed requeue with no error so
// the controller-runtime backoff does not override the delay and the error is
// not surfaced as a reconcile failure. All other errors pass through.
func resultAfterError(key types.NamespacedName, err error) (ctrl.Result, error) {
	if clouderrors.IsThrottled(err) || clouderrors.IsQuotaExceeded(err) {
		return ctrl.Result{RequeueAfter: requeueAfterForError(key, err)}, nil
	}
	return ctrl.Result{}, err
}

// resetBackoff clears the exponential-backoff counter for a reconcile key
// (called after a clean reconcile so the next transient failure starts over).
func resetBackoff(key types.NamespacedName) {
	errorBackoff.reset(key)
}
