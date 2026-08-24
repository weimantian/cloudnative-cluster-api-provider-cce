/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

// validCluster returns a cluster that passes base validation.
func validCluster() *CCECluster {
	return &CCECluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cce1", Namespace: "default"},
		Spec: CCEClusterSpec{
			Region: "cn-north-4",
		},
	}
}

// TestCCEClusterValidateNetworkCIDR covers the VPC and subnet CIDR format
// checks added in P1-#5.
func TestCCEClusterValidateNetworkCIDR(t *testing.T) {
	ok := validCluster()
	ok.Spec.Network = common.NetworkSpec{
		VPC:     common.VPC{CIDR: "10.0.0.0/16"},
		Subnets: []common.Subnet{{CIDR: "10.0.1.0/24"}, {CIDR: "10.0.2.0/24"}},
	}
	if err := ok.validate(); err != nil {
		t.Errorf("expected valid network CIDRs, got %v", err)
	}

	badVPC := validCluster()
	badVPC.Spec.Network = common.NetworkSpec{VPC: common.VPC{CIDR: "10.0.0.999/16"}}
	if err := badVPC.validate(); err == nil {
		t.Error("expected invalid VPC CIDR to be rejected")
	}

	badSubnet := validCluster()
	badSubnet.Spec.Network = common.NetworkSpec{Subnets: []common.Subnet{{CIDR: "nope"}}}
	if err := badSubnet.validate(); err == nil {
		t.Error("expected invalid subnet CIDR to be rejected")
	}
}

// TestCCEClusterValidateUpdate covers region and VPC ID immutability.
func TestCCEClusterValidateUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("region immutable", func(t *testing.T) {
		old := validCluster()
		new := validCluster()
		new.Spec.Region = "cn-north-1"
		if _, err := old.ValidateUpdate(ctx, old, new); err == nil {
			t.Error("expected region change to be rejected")
		}
	})

	t.Run("vpc id immutable once set", func(t *testing.T) {
		old := validCluster()
		old.Spec.Network = common.NetworkSpec{VPC: common.VPC{ID: "vpc-1"}}
		new := validCluster()
		new.Spec.Network = common.NetworkSpec{VPC: common.VPC{ID: "vpc-2"}}
		if _, err := old.ValidateUpdate(ctx, old, new); err == nil {
			t.Error("expected VPC ID change to be rejected")
		}
	})

	t.Run("vpc id settable when previously empty", func(t *testing.T) {
		old := validCluster()
		new := validCluster()
		new.Spec.Network = common.NetworkSpec{VPC: common.VPC{ID: "vpc-1"}}
		if _, err := old.ValidateUpdate(ctx, old, new); err != nil {
			t.Errorf("expected VPC ID set on empty to be accepted, got %v", err)
		}
	})
}
