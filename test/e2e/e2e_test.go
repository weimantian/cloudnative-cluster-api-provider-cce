/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package e2e contains end-to-end tests that drive a real management cluster
// and a real Huawei Cloud CCE account. The suite is a placeholder in the PoC
// skeleton; the actual scenarios (create → ready → scale → delete, upgrade)
// will be implemented on top of the CAPI e2e framework
// (sigs.k8s.io/cluster-api/test) — see docs/requirements-design.md FR-10.4.
package e2e

import (
	"testing"
)

func TestE2EPlaceholder(t *testing.T) {
	t.Skip("e2e requires a management cluster and real CCE credentials; implemented in P0 (docs/requirements-design.md FR-10.4)")
}
