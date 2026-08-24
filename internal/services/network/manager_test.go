/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"testing"

	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

// TestHasOwnedTag verifies the owned-tag adoption marker detection.
func TestHasOwnedTag(t *testing.T) {
	cases := []struct {
		name string
		tags common.Tags
		want bool
	}{
		{name: "owned", tags: common.Tags{"cluster-api-provider-cce.cluster.foo": "owned"}, want: true},
		{name: "shared value", tags: common.Tags{"cluster-api-provider-cce.cluster.foo": "shared"}, want: false},
		{name: "unrelated", tags: common.Tags{"foo": "owned"}, want: false},
		{name: "empty", tags: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasOwnedTag(tc.tags, "foo"); got != tc.want {
				t.Errorf("HasOwnedTag(%v, foo) = %v, want %v", tc.tags, got, tc.want)
			}
		})
	}
}

// TestIsManaged verifies the three-state model: create (vpc.id empty),
// adopt (vpc.id + owned tag), BYO (vpc.id + no tag).
func TestIsManaged(t *testing.T) {
	cases := []struct {
		name string
		spec *common.NetworkSpec
		want bool
	}{
		{name: "nil", spec: nil, want: false},
		{name: "create by cidr", spec: &common.NetworkSpec{VPC: common.VPC{CIDR: "10.0.0.0/16"}}, want: true},
		{name: "already created", spec: &common.NetworkSpec{VPC: common.VPC{ResourceID: "vpc-1"}}, want: true},
		{name: "adopt", spec: &common.NetworkSpec{VPC: common.VPC{ID: "vpc-1", Tags: common.Tags{"cluster-api-provider-cce.cluster.foo": "owned"}}}, want: true},
		{name: "byo", spec: &common.NetworkSpec{VPC: common.VPC{ID: "vpc-1"}}, want: false},
		{name: "byo shared tag", spec: &common.NetworkSpec{VPC: common.VPC{ID: "vpc-1", Tags: common.Tags{"cluster-api-provider-cce.cluster.foo": "shared"}}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsManaged(tc.spec, "foo"); got != tc.want {
				t.Errorf("IsManaged(%v, foo) = %v, want %v", tc.spec, tc.want, got)
			}
		})
	}
}

// TestOwnedTagKey verifies the owned tag key construction.
func TestOwnedTagKey(t *testing.T) {
	if got := ownedTagKey("my-cluster"); got != "cluster-api-provider-cce.cluster.my-cluster" {
		t.Errorf("ownedTagKey(my-cluster) = %q, want %q", got, "cluster-api-provider-cce.cluster.my-cluster")
	}
}

// TestSecurityGroupRuleExists verifies the idempotent rule matching used by
// ensureSecurityGroupRules (direction + protocol + port range + remote).
func TestSecurityGroupRuleExists(t *testing.T) {
	existing := []vpcmodel.SecurityGroupRule{
		{Direction: "ingress", Protocol: "tcp", PortRangeMin: 22, PortRangeMax: 22, RemoteIpPrefix: "10.0.0.0/8"},
		{Direction: "egress", Protocol: "", PortRangeMin: 0, PortRangeMax: 0, RemoteIpPrefix: "0.0.0.0/0"},
	}
	cases := []struct {
		name      string
		direction string
		rule      common.SecurityGroupRuleSpec
		want      bool
	}{
		{"exact match", "ingress", common.SecurityGroupRuleSpec{Protocol: "tcp", PortRangeMin: 22, PortRangeMax: 22, RemoteIPPrefix: "10.0.0.0/8"}, true},
		{"protocol differs", "ingress", common.SecurityGroupRuleSpec{Protocol: "udp", PortRangeMin: 22, PortRangeMax: 22, RemoteIPPrefix: "10.0.0.0/8"}, false},
		{"port differs", "ingress", common.SecurityGroupRuleSpec{Protocol: "tcp", PortRangeMin: 80, PortRangeMax: 80, RemoteIPPrefix: "10.0.0.0/8"}, false},
		{"remote differs", "ingress", common.SecurityGroupRuleSpec{Protocol: "tcp", PortRangeMin: 22, PortRangeMax: 22, RemoteIPPrefix: "192.168.0.0/16"}, false},
		{"direction differs", "egress", common.SecurityGroupRuleSpec{Protocol: "tcp", PortRangeMin: 22, PortRangeMax: 22, RemoteIPPrefix: "10.0.0.0/8"}, false},
		{"all-protocol egress", "egress", common.SecurityGroupRuleSpec{RemoteIPPrefix: "0.0.0.0/0"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := securityGroupRuleExists(existing, tc.direction, tc.rule); got != tc.want {
				t.Errorf("securityGroupRuleExists(%q, %+v) = %v, want %v", tc.direction, tc.rule, got, tc.want)
			}
		})
	}
}
