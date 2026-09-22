/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package throttle

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// Client-side throttling for the Huawei Cloud clients (CCE + managed-network
// VPC/NAT/EIP), using a token-bucket limiter split by HTTP method so that
// status polling (GET) is never delayed by the far stricter write limit Huawei
// Cloud enforces.
//
// Rates:
//   - reads (GET/HEAD) share a generous bucket (20 ops/s, burst 100) so the
//     5s NAT-gateway poll is never throttled;
//   - writes (everything else: Create/Delete) are clamped to the observed
//     APIGW.0308 limit of 10 requests/minute (one token every 6s, burst 10).
//     The burst covers a single managed-network create (VPC + 2 subnets + NAT
//     + EIP + SNAT ≈ 6 writes), which are issued serially with polling gaps in
//     between, so the burst is effectively never exhausted in normal use.
//
// The limiter is process-level shared: every client draws from the same
// read/write buckets via Shared(), so the aggregate write budget stays at the
// 10 writes/min platform cap instead of being per-client (per-client buckets
// would let several clients collectively exceed the cap the limiter exists to
// respect).
const (
	readThrottleRate  = 20.0 // operations per second
	readThrottleBurst = 100

	writeThrottleInterval = 6 * time.Second // 10 requests per minute
	writeThrottleBurst    = 10
)

// shared is the process-wide limiter shared by every Huawei Cloud client.
var shared = NewOperationLimiter()

// Shared returns the process-wide OperationLimiter shared by all clients, so
// the read/write budget is a single global bucket rather than per-client.
func Shared() *OperationLimiter { return shared }

// OperationLimiter is a token-bucket limiter with independent read and write
// buckets. It is safe for concurrent use; the underlying rate.Limiter handles
// its own locking.
type OperationLimiter struct {
	read  *rate.Limiter
	write *rate.Limiter
}

// NewOperationLimiter builds a limiter with the default read/write rates.
func NewOperationLimiter() *OperationLimiter {
	return &OperationLimiter{
		read:  rate.NewLimiter(rate.Limit(readThrottleRate), readThrottleBurst),
		write: rate.NewLimiter(rate.Every(writeThrottleInterval), writeThrottleBurst),
	}
}

// wait blocks until a token is available for the given HTTP method, or ctx is
// cancelled. GET/HEAD count as reads; every other method is a write.
func (l *OperationLimiter) wait(ctx context.Context, method string) error {
	if method == http.MethodGet || method == http.MethodHead {
		return l.read.Wait(ctx)
	}
	return l.write.Wait(ctx)
}

// ThrottleRoundTripper is an http.RoundTripper that applies the read/write
// limiter before delegating to the wrapped transport.
type ThrottleRoundTripper struct {
	base    http.RoundTripper
	limiter *OperationLimiter
}

// NewThrottleRoundTripper wraps base with the supplied limiter. The returned
// RoundTripper is safe for concurrent use. Pass http.DefaultTransport as base
// to throttle Huawei Cloud SDK HTTP clients.
func NewThrottleRoundTripper(base http.RoundTripper, limiter *OperationLimiter) *ThrottleRoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &ThrottleRoundTripper{base: base, limiter: limiter}
}

// RoundTrip implements http.RoundTripper.
func (t *ThrottleRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.wait(req.Context(), req.Method); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}
