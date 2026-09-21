/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package wait provides utilities for polling and waiting on cloud resources
// with exponential backoff. Adapted from the Cluster API provider reference
// implementation.
package wait

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// NewBackoff returns an exponential backoff configuration suitable for polling
// cloud resources. Total wall time ~5 minutes; example durations without jitter:
//
//	1.0s, 1.7s, 2.9s, 5.0s, 8.6s, 14.6s, 25.0s, 42.8s, 73.1s, 125.0s
//
// Jitter is added as a random fraction of each duration (factor 0.4).
func NewBackoff() wait.Backoff {
	return wait.Backoff{
		Duration: time.Second,
		Factor:   1.71,
		Steps:    10,
		Jitter:   0.4,
	}
}

// WaitForWithRetryable repeatedly evaluates condition with exponential backoff
// until the condition returns true, a condition error occurs, or the
// backoff budget is exhausted. ctx is honored: cancellation returns
// ctx.Err() immediately.
//
// A condition error is returned immediately (it is never retried).
func WaitForWithRetryable(ctx context.Context, backoff wait.Backoff, condition wait.ConditionFunc) error {
	var lastErr error
	waitErr := wait.ExponentialBackoff(backoff, func() (bool, error) {
		lastErr = nil

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		ok, err := condition()
		if ok {
			return true, nil
		}
		if err == nil {
			return false, nil
		}

		lastErr = err
		return false, err // propagate immediately
	})

	if waitErr == nil {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return waitErr
}
