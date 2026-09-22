/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"testing"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
)

// NewValidator builds a VPC client and, on the success path, resolves the
// account's project ID via a live keystone call — so only the pre-network
// failure branches (region resolution, credential build) are testable offline.
// The success path is exercised only against a real account and is noted in
// the quality-audit report as uncovered without a seam.
func TestNewValidator(t *testing.T) {
	t.Run("unknown region errors before network", func(t *testing.T) {
		if _, err := NewValidator("not-a-region", &credentials.Credentials{AccessKey: "ak", SecretKey: "sk"}); err == nil {
			t.Fatal("NewValidator(unknown region) expected error, got nil")
		}
	})

	t.Run("empty credentials errors before network", func(t *testing.T) {
		if _, err := NewValidator("cn-north-4", &credentials.Credentials{}); err == nil {
			t.Fatal("NewValidator(empty credentials) expected error, got nil")
		}
	})
}
