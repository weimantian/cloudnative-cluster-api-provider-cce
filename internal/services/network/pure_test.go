/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"testing"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

func TestGatewayIP(t *testing.T) {
	cases := []struct {
		cidr string
		want string
	}{
		{cidr: "10.0.1.0/24", want: "10.0.1.1"},
		{cidr: "10.0.0.0/16", want: "10.0.0.1"},
		{cidr: "fd00::/64", want: "fd00::1"},
		{cidr: "not-a-cidr", want: ""},
		{cidr: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.cidr, func(t *testing.T) {
			if got := gatewayIP(tc.cidr); got != tc.want {
				t.Errorf("gatewayIP(%q) = %q, want %q", tc.cidr, got, tc.want)
			}
		})
	}
}

func TestNatSpecEnum(t *testing.T) {
	cases := []struct {
		spec string
		want string
	}{
		{spec: "1", want: "1"},
		{spec: "2", want: "2"},
		{spec: "3", want: "3"},
		{spec: "4", want: "4"},
		{spec: "5", want: "1"}, // no "5" case; falls to default E_1
		{spec: "", want: "1"},
		{spec: "bogus", want: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			got := natSpecEnum(tc.spec)
			if got.Value() != tc.want {
				t.Errorf("natSpecEnum(%q) = %q, want %q", tc.spec, got.Value(), tc.want)
			}
		})
	}
}

func TestCidrsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{a: "10.0.0.0/16", b: "10.0.1.0/24", want: true},
		{a: "10.0.0.0/24", b: "10.1.0.0/24", want: false},
		{a: "10.0.0.0/24", b: "10.0.0.0/24", want: true},
		{a: "10.0.0.0/16", b: "not-a-cidr", want: false},
		{a: "not-a-cidr", b: "10.0.0.0/16", want: false},
		{a: "", b: "10.0.0.0/16", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			if got := cidrsOverlap(tc.a, tc.b); got != tc.want {
				t.Errorf("cidrsOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestValidCIDR(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{in: "10.0.0.0/16", want: true},
		{in: "fd00::/64", want: true},
		{in: "10.0.0.0", want: false},
		{in: "", want: false},
		{in: "300.0.0.0/8", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := validCIDR(tc.in); got != tc.want {
				t.Errorf("validCIDR(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFirstManagedNodeSubnet(t *testing.T) {
	cases := []struct {
		name string
		spec *common.NetworkSpec
		want string
	}{
		{
			name: "first managed node subnet",
			spec: &common.NetworkSpec{Subnets: []common.Subnet{
				{Type: common.SubnetTypeNode, ResourceID: "res-1"},
				{Type: common.SubnetTypeNode, ResourceID: "res-2"},
			}},
			want: "res-1",
		},
		{
			name: "skips byo and eni subnets",
			spec: &common.NetworkSpec{Subnets: []common.Subnet{
				{ID: "byo-1", Type: common.SubnetTypeNode},
				{Type: common.SubnetTypeENI, ResourceID: "eni-1"},
				{Type: common.SubnetTypeNode, ResourceID: "res-1"},
			}},
			want: "res-1",
		},
		{
			name: "skips managed subnet without resource id",
			spec: &common.NetworkSpec{Subnets: []common.Subnet{
				{Type: common.SubnetTypeNode, ResourceID: ""},
				{Type: common.SubnetTypeNode, ResourceID: "res-1"},
			}},
			want: "res-1",
		},
		{name: "empty", spec: &common.NetworkSpec{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstManagedNodeSubnet(tc.spec); got != tc.want {
				t.Errorf("firstManagedNodeSubnet() = %q, want %q", got, tc.want)
			}
		})
	}
}
