/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"errors"
	"testing"
	"time"

	sdkerr "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	"k8s.io/apimachinery/pkg/types"
)

func TestBackoffTrackerDelay(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "test-cluster"}
	b := newBackoffTracker()

	// Consecutive failures double the delay, capped at backoffMax.
	want := []time.Duration{
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		8 * time.Minute,
		16 * time.Minute,
		30 * time.Minute, // capped at backoffMax
		30 * time.Minute, // stays capped
	}
	for i, w := range want {
		if got := b.delay(key, time.Minute); got != w {
			t.Fatalf("failure %d: delay() = %v, want %v", i+1, got, w)
		}
	}

	// Reset clears the counter so the next failure starts from base again.
	b.reset(key)
	if got := b.delay(key, time.Minute); got != time.Minute {
		t.Fatalf("after reset: delay() = %v, want %v", got, time.Minute)
	}
}

func TestRequeueAfterForErrorExponential(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "throttled-cluster"}
	resetBackoff(key)
	defer resetBackoff(key)

	throttled := &sdkerr.ServiceResponseError{StatusCode: 429}
	// 429 uses a 3-minute base (> the 1-minute platform write window, so retries
	// let the window drain instead of re-hitting it): base, 2x, 4x.
	if got := requeueAfterForError(key, throttled); got != throttledBackoffBase {
		t.Fatalf("1st throttled: got %v, want %v", got, throttledBackoffBase)
	}
	if got := requeueAfterForError(key, throttled); got != 2*throttledBackoffBase {
		t.Fatalf("2nd throttled: got %v, want %v", got, 2*throttledBackoffBase)
	}
	if got := requeueAfterForError(key, throttled); got != 4*throttledBackoffBase {
		t.Fatalf("3rd throttled: got %v, want %v", got, 4*throttledBackoffBase)
	}
}

func TestRequeueAfterForErrorClasses(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "classes"}
	resetBackoff(key)
	defer resetBackoff(key)

	quota := &sdkerr.ServiceResponseError{ErrorCode: "CCE.01400007"} // InsufficientClusterQuota
	if got := requeueAfterForError(key, quota); got != 5*time.Minute {
		t.Fatalf("quota: got %v, want %v", got, 5*time.Minute)
	}

	permission := &sdkerr.ServiceResponseError{StatusCode: 401}
	if got := requeueAfterForError(key, permission); got != 30*time.Minute {
		t.Fatalf("permission: got %v, want %v", got, 30*time.Minute)
	}

	other := errors.New("boom") // not a classified error
	if got := requeueAfterForError(key, other); got != defaultRequeue {
		t.Fatalf("default: got %v, want %v", got, defaultRequeue)
	}
}

func TestResultAfterError(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "result"}
	resetBackoff(key)
	defer resetBackoff(key)

	throttled := &sdkerr.ServiceResponseError{StatusCode: 429}
	res, err := resultAfterError(key, throttled)
	if err != nil {
		t.Fatalf("throttled should not surface an error, got %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Fatal("throttled should requeue after a delay")
	}

	other := errors.New("boom")
	res, err = resultAfterError(key, other)
	if err == nil {
		t.Fatal("non-transient error should pass through")
	}
	if res.RequeueAfter != 0 {
		t.Fatalf("non-transient error should not requeue, got %v", res.RequeueAfter)
	}
}

// TestResultAfterErrorPermissionParked covers B12: a permission error on the
// post-Available path must park on the fixed long permissionBackoff instead of
// being surfaced as a fast-retried reconcile error.
func TestResultAfterErrorPermissionParked(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "permission-parked"}
	resetBackoff(key)
	defer resetBackoff(key)

	permission := &sdkerr.ServiceResponseError{StatusCode: 403, ErrorCode: "CCE.01403001"}
	res, err := resultAfterError(key, permission)
	if err != nil {
		t.Fatalf("permission error must be parked, not surfaced: %v", err)
	}
	if res.RequeueAfter != permissionBackoff {
		t.Fatalf("permission: RequeueAfter = %v, want %v", res.RequeueAfter, permissionBackoff)
	}
}

func TestResultAfterErrorForDelete(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "delete-target"}
	resetBackoff(key)
	defer resetBackoff(key)

	throttled := &sdkerr.ServiceResponseError{StatusCode: 429}
	// Deletion uses the shorter operator-facing backoff (90s), doubling on repeat.
	res, err := resultAfterErrorForDelete(key, throttled)
	if err != nil || res.RequeueAfter != deletedThrottledBackoffBase {
		t.Fatalf("1st delete throttle: got %v err=%v, want %v", res.RequeueAfter, err, deletedThrottledBackoffBase)
	}
	res, err = resultAfterErrorForDelete(key, throttled)
	if err != nil || res.RequeueAfter != 2*deletedThrottledBackoffBase {
		t.Fatalf("2nd delete throttle: got %v err=%v, want %v", res.RequeueAfter, err, 2*deletedThrottledBackoffBase)
	}
	// Non-throttled errors pass through unchanged.
	if _, e := resultAfterErrorForDelete(key, errors.New("boom")); e == nil {
		t.Error("non-throttled error must pass through as an error")
	}
}
