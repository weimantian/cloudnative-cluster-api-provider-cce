/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package wait provides utilities for polling and waiting on cloud resources
// with exponential backoff. Adapted from CAPA v2.13.0
// pkg/cloud/services/wait/wait.go (commit history). Stripped of AWS-specific
// awserrors dependency — uses errors.Cause() + string match so it works
// against any SDK (CCE's pkg/errors-based error wrapping is compatible).
package wait

import (
	"context"
	"time"

	"github.com/pkg/errors"
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
// until the condition returns true, a non-retryable error occurs, or the
// backoff budget is exhausted. ctx is honored: cancellation returns
// ctx.Err() immediately.
//
// retryableErrors is a list of error message strings (compared against
// errors.Cause(err).Error()) that should be retried instead of returned
// immediately. Pass nil/empty to disable retry classification.
func WaitForWithRetryable(ctx context.Context, backoff wait.Backoff, condition wait.ConditionFunc, retryableErrors ...string) error {
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
		for _, r := range retryableErrors {
			if errors.Cause(err).Error() == r {
				return false, nil // retryable
			}
		}
		return false, err // non-retryable, propagate immediately
	})

	if waitErr == nil {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return waitErr
}