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
	// 1st: base (1m); 2nd: doubled (2m); 3rd: 4m.
	if got := requeueAfterForError(key, throttled); got != time.Minute {
		t.Fatalf("1st throttled: got %v, want %v", got, time.Minute)
	}
	if got := requeueAfterForError(key, throttled); got != 2*time.Minute {
		t.Fatalf("2nd throttled: got %v, want %v", got, 2*time.Minute)
	}
	if got := requeueAfterForError(key, throttled); got != 4*time.Minute {
		t.Fatalf("3rd throttled: got %v, want %v", got, 4*time.Minute)
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
