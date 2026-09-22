/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"context"
	"net/http"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

// newTestValidator builds a Validator whose VPC client talks to rt instead of
// Huawei Cloud (mirrors newAdoptionTestManager, VPC client only).
func newTestValidator(t *testing.T, rt http.RoundTripper) *Validator {
	t.Helper()
	cred, err := basic.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithProjectId("project-test").SafeBuild()
	if err != nil {
		t.Fatalf("build fake credentials: %v", err)
	}
	httpConfig := config.DefaultHttpConfig().WithHttpRoundTripper(rt)
	vpcRegion, err := vpcregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve vpc region: %v", err)
	}
	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(vpcRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build vpc client: %v", err)
	}
	return &Validator{vpc: vpcv2.NewVpcClient(vpcHC)}
}

// vpcRoutes serves the canned VPC/subnet facts for VPC vpc-1 (CIDR 10.0.0.0/16)
// containing subnet sub-1 (CIDR 10.0.1.0/24).
func vpcRoutes() []fakeRoute {
	return []fakeRoute{
		{method: http.MethodGet, pathSub: "/vpcs", body: `{"vpcs":[{"id":"vpc-1","cidr":"10.0.0.0/16"}]}`},
		{method: http.MethodGet, pathSub: "/subnets", body: `{"subnets":[{"id":"sub-1","cidr":"10.0.1.0/24","neutron_subnet_id":"neu-1"}]}`},
	}
}

// TestValidate exercises the network checks against cloud facts fetched through
// a canned VPC API (no real Huawei Cloud calls).
func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		in         ValidateInput
		wantFields []string // expected issue fields (non-warning)
		wantWarn   int
	}{
		{
			name: "valid vpc-router network",
			in: ValidateInput{
				VPCID:         "vpc-1",
				SubnetIDs:     []string{"sub-1"},
				ContainerMode: "vpc-router",
				ContainerCIDR: "10.244.0.0/16",
				ServiceCIDR:   "10.247.0.0/16",
			},
		},
		{
			name: "service CIDR overlaps VPC",
			in: ValidateInput{
				VPCID:         "vpc-1",
				SubnetIDs:     []string{"sub-1"},
				ContainerMode: "overlay_l2",
				ServiceCIDR:   "10.0.0.0/24", // inside VPC 10.0.0.0/16
			},
			wantFields: []string{"serviceNetwork.cidr"},
		},
		{
			name: "missing subnet and eni overlap warning",
			in: ValidateInput{
				VPCID:         "vpc-1",
				SubnetIDs:     []string{"sub-1", "sub-missing"},
				ContainerMode: "eni",
				ENISubnetIDs:  []string{"sub-1"},
				ServiceCIDR:   "10.247.0.0/16",
			},
			wantFields: []string{"network.subnets"},
			wantWarn:   1, // sub-1 shared as node + eni subnet
		},
		{
			name: "container overlaps service (vpc-router)",
			in: ValidateInput{
				VPCID:         "vpc-1",
				SubnetIDs:     []string{"sub-1"},
				ContainerMode: "vpc-router",
				ContainerCIDR: "10.247.0.0/16",
				ServiceCIDR:   "10.247.128.0/17",
			},
			wantFields: []string{"containerNetwork.cidr"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &fakeRoundTripper{t: t, routes: vpcRoutes()}
			v := newTestValidator(t, rt)
			issues, err := v.Validate(context.Background(), tt.in)
			if err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
			var hard []string
			warnings := 0
			for _, i := range issues {
				if i.Warning {
					warnings++
					continue
				}
				hard = append(hard, i.Field)
			}
			for _, want := range tt.wantFields {
				found := false
				for _, got := range hard {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected hard issue on field %q, got %v", want, hard)
				}
			}
			if len(hard) != len(tt.wantFields) {
				t.Errorf("expected %d hard issues, got %v", len(tt.wantFields), hard)
			}
			if warnings != tt.wantWarn {
				t.Errorf("expected %d warnings, got %d (%v)", tt.wantWarn, warnings, issues)
			}
		})
	}
}

func TestCIDRsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"10.0.0.0/16", "10.0.1.0/24", true},
		{"10.0.0.0/16", "10.247.0.0/16", false},
		{"10.244.0.0/16", "10.244.128.0/17", true},
		{"invalid", "10.0.0.0/16", false},
	}
	for _, c := range cases {
		if got := cidrsOverlap(c.a, c.b); got != c.want {
			t.Errorf("cidrsOverlap(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestValidateInvalidCIDR(t *testing.T) {
	v := &Validator{} // nil vpc => no cloud fetch; the CIDR-format check runs first
	issues, err := v.Validate(context.Background(), ValidateInput{
		ServiceCIDR: "not-a-cidr",
	})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	found := false
	for _, i := range issues {
		if i.Field == "serviceNetwork.cidr" && i.Message == "invalid CIDR: not-a-cidr" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected invalid service CIDR issue, got %+v", issues)
	}
}

func TestValidateEniRequiresSubnets(t *testing.T) {
	v := &Validator{}
	issues, err := v.Validate(context.Background(), ValidateInput{
		ContainerMode: "eni",
		ENISubnetIDs:  []string{},
	})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	found := false
	for _, i := range issues {
		if i.Field == "containerNetwork.eniSubnets" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected eni-no-subnets issue, got %+v", issues)
	}
}
