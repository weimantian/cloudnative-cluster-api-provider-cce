/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// Client-side throttling for the managed-network clients (VPC/NAT/EIP), mirroring
// CAPA's token-bucket limiter but split by HTTP method so that status polling
// (GET) is never delayed by the far stricter write limit Huawei Cloud enforces.
//
// Rates:
//   - reads (GET/HEAD) share a generous bucket (20 ops/s, burst 100) so the
//     5s NAT-gateway poll is never throttled;
//   - writes (everything else: Create/Delete) are clamped to the observed
//     APIGW.0308 limit of 10 requests/minute (one token every 6s, burst 10).
//     The burst covers a single managed-network create (VPC + 2 subnets + NAT
//     + EIP + SNAT ≈ 6 writes), which are issued serially with polling gaps in
//     between, so the burst is effectively never exhausted in normal use.
const (
	readThrottleRate  = 20.0 // operations per second
	readThrottleBurst = 100

	writeThrottleInterval = 6 * time.Second // 10 requests per minute
	writeThrottleBurst    = 10
)

// operationLimiter is a token-bucket limiter with independent read and write
// buckets. It is safe for concurrent use; the underlying rate.Limiter handles
// its own locking.
type operationLimiter struct {
	read  *rate.Limiter
	write *rate.Limiter
}

// newOperationLimiter builds a limiter with the default read/write rates.
func newOperationLimiter() *operationLimiter {
	return &operationLimiter{
		read:  rate.NewLimiter(rate.Limit(readThrottleRate), readThrottleBurst),
		write: rate.NewLimiter(rate.Every(writeThrottleInterval), writeThrottleBurst),
	}
}

// wait blocks until a token is available for the given HTTP method, or ctx is
// cancelled. GET/HEAD count as reads; every other method is a write.
func (l *operationLimiter) wait(ctx context.Context, method string) error {
	if method == http.MethodGet || method == http.MethodHead {
		return l.read.Wait(ctx)
	}
	return l.write.Wait(ctx)
}

// throttleRoundTripper is an http.RoundTripper that applies the read/write
// limiter before delegating to the wrapped transport. It wraps the shared
// http.DefaultTransport, which is safe to reuse across the VPC/NAT/EIP clients.
type throttleRoundTripper struct {
	base    http.RoundTripper
	limiter *operationLimiter
}

// newThrottleRoundTripper wraps base with the rate limiter.
func newThrottleRoundTripper(base http.RoundTripper, limiter *operationLimiter) *throttleRoundTripper {
	return &throttleRoundTripper{base: base, limiter: limiter}
}

// RoundTrip implements http.RoundTripper.
func (t *throttleRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.wait(req.Context(), req.Method); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}
