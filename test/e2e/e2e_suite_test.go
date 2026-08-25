//go:build e2e
// +build e2e

/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package e2e contains the Ginkgo-based end-to-end suite that drives a real
// management cluster and a real Huawei Cloud CCE account (mirrors the CAPA
// e2e suite shape: build tag `e2e`, Ginkgo RunSpecs entry, env-gated specs).
//
// Run with: go test -tags e2e -timeout 60m ./test/e2e/...
package e2e

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestE2E is the Ginkgo suite entry point (mirrors CAPA's TestE2E).
func TestE2E(t *testing.T) {
	ctrl.SetLogger(klog.Background())
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "capi-cce-e2e")
}
