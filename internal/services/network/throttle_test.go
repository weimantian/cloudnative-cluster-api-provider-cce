/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"context"
	"net/http"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestOperationLimiterDefaults verifies the limiter is wired with the intended
// read/write rates and bursts.
func TestOperationLimiterDefaults(t *testing.T) {
	l := newOperationLimiter()

	if got := l.read.Burst(); got != readThrottleBurst {
		t.Errorf("read burst = %d, want %d", got, readThrottleBurst)
	}
	if got := l.write.Burst(); got != writeThrottleBurst {
		t.Errorf("write burst = %d, want %d", got, writeThrottleBurst)
	}
	if got := l.read.Limit(); got != rate.Limit(readThrottleRate) {
		t.Errorf("read rate = %v, want %v", got, rate.Limit(readThrottleRate))
	}
	if got := l.write.Limit(); got != rate.Every(writeThrottleInterval) {
		t.Errorf("write rate = %v, want %v", got, rate.Every(writeThrottleInterval))
	}
}

// TestOperationLimiterThrottlesWrites verifies that once the write burst is
// exhausted, the next write blocks (waiting for the 6s refill) rather than
// proceeding immediately.
func TestOperationLimiterThrottlesWrites(t *testing.T) {
	l := newOperationLimiter()
	ctx := context.Background()

	for i := 0; i < writeThrottleBurst; i++ {
		if err := l.wait(ctx, http.MethodPost); err != nil {
			t.Fatalf("write %d: unexpected err %v", i, err)
		}
	}

	// The write bucket is now empty; the next write must wait for the refill
	// (writeThrottleInterval). A short window confirms it does not complete.
	done := make(chan error, 1)
	go func() { done <- l.wait(ctx, http.MethodPost) }()
	select {
	case err := <-done:
		t.Fatalf("write completed after burst was exhausted: %v", err)
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked awaiting a token
	}
}

// TestOperationLimiterMethodIsolation verifies reads and writes use separate
// buckets: exhausting the write bucket must not throttle a read.
func TestOperationLimiterMethodIsolation(t *testing.T) {
	l := newOperationLimiter()
	ctx := context.Background()

	for i := 0; i < writeThrottleBurst; i++ {
		if err := l.wait(ctx, http.MethodPost); err != nil {
			t.Fatalf("write %d: unexpected err %v", i, err)
		}
	}

	// A GET must still succeed immediately despite the empty write bucket.
	done := make(chan error, 1)
	go func() { done <- l.wait(ctx, http.MethodGet) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GET throttled by exhausted write bucket: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("GET did not complete: read bucket was incorrectly throttled")
	}
}

// TestOperationLimiterWaitCancellation verifies an already-cancelled context
// short-circuits the wait with the context error.
func TestOperationLimiterWaitCancellation(t *testing.T) {
	l := newOperationLimiter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := l.wait(ctx, http.MethodPost); err == nil {
		t.Fatal("wait with cancelled context returned nil error")
	}
}

// stubRoundTripper records invocations and returns a canned response.
type stubRoundTripper struct {
	calls  int
	status int
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.calls++
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
}

// TestThrottleRoundTripperDelegates verifies the wrapper passes the request
// through to the base transport after the limiter grants a token.
func TestThrottleRoundTripperDelegates(t *testing.T) {
	base := &stubRoundTripper{status: http.StatusOK}
	rt := newThrottleRoundTripper(base, newOperationLimiter())

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if base.calls != 1 {
		t.Errorf("base calls = %d, want 1", base.calls)
	}
}
