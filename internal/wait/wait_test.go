/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package wait

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

var errNonRetryable = errors.New("non retryable error")

func fastBackoff() wait.Backoff {
	return wait.Backoff{
		Duration: 1 * time.Millisecond,
		Factor:   0,
		Jitter:   0,
		Steps:    3,
		Cap:      3 * time.Millisecond,
	}
}

func TestNewBackoff(t *testing.T) {
	b := NewBackoff()
	if b.Duration != time.Second {
		t.Errorf("expected Duration=1s, got %v", b.Duration)
	}
	if b.Steps != 10 {
		t.Errorf("expected Steps=10, got %d", b.Steps)
	}
	if b.Jitter != 0.4 {
		t.Errorf("expected Jitter=0.4, got %v", b.Jitter)
	}
	if b.Factor != 1.71 {
		t.Errorf("expected Factor=1.71, got %v", b.Factor)
	}
}

func TestWaitForWithRetryable_ReturnsNilOnFirstTrue(t *testing.T) {
	calls := 0
	err := WaitForWithRetryable(context.Background(), fastBackoff(), func() (bool, error) {
		calls++
		return true, nil
	})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestWaitForWithRetryable_Timeout(t *testing.T) {
	err := WaitForWithRetryable(context.Background(), fastBackoff(), func() (bool, error) {
		return false, nil
	})
	if !errors.Is(err, wait.ErrWaitTimeout) {
		t.Errorf("expected ErrWaitTimeout, got %v", err)
	}
}

func TestWaitForWithRetryable_NonRetryableImmediateReturn(t *testing.T) {
	calls := 0
	err := WaitForWithRetryable(context.Background(), fastBackoff(), func() (bool, error) {
		calls++
		return false, errNonRetryable
	})
	if err != errNonRetryable {
		t.Errorf("expected errNonRetryable, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (immediate return), got %d", calls)
	}
}

func TestWaitForWithRetryable_ContextCancelled(t *testing.T) {
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WaitForWithRetryable(cancelledCtx, fastBackoff(), func() (bool, error) {
		return false, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
