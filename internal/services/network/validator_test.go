/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"context"
	"testing"
)

// TestValidateUsesCloudFacts exercises the pure checks with cloud facts
// injected directly (no VPC API call: validator is nil).
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
				VPCID:            "vpc-1",
				SubnetIDs:        []string{"sub-1"},
				ContainerMode:    "vpc-router",
				ContainerCIDR:    "10.244.0.0/16",
				ServiceCIDR:      "10.247.0.0/16",
				ENISubnetIDs:     nil,
				VPCCloudCIDR:     "10.0.0.0/16",
				CloudSubnetCIDRs: map[string]string{"sub-1": "10.0.1.0/24"},
			},
		},
		{
			name: "service CIDR overlaps VPC",
			in: ValidateInput{
				VPCID:            "vpc-1",
				SubnetIDs:        []string{"sub-1"},
				ContainerMode:    "overlay_l2",
				ServiceCIDR:      "10.0.0.0/24", // inside VPC 10.0.0.0/16
				VPCCloudCIDR:     "10.0.0.0/16",
				CloudSubnetCIDRs: map[string]string{"sub-1": "10.0.1.0/24"},
			},
			wantFields: []string{"serviceNetwork.cidr"},
		},
		{
			name: "missing subnet and eni overlap warning",
			in: ValidateInput{
				VPCID:            "vpc-1",
				SubnetIDs:        []string{"sub-1", "sub-missing"},
				ContainerMode:    "eni",
				ENISubnetIDs:     []string{"sub-1"},
				ServiceCIDR:      "10.247.0.0/16",
				VPCCloudCIDR:     "10.0.0.0/16",
				CloudSubnetCIDRs: map[string]string{"sub-1": "10.0.1.0/24"},
			},
			wantFields: []string{"network.subnets"},
			wantWarn:   1, // sub-1 shared as node + eni subnet
		},
		{
			name: "container overlaps service (vpc-router)",
			in: ValidateInput{
				VPCID:            "vpc-1",
				SubnetIDs:        []string{"sub-1"},
				ContainerMode:    "vpc-router",
				ContainerCIDR:    "10.247.0.0/16",
				ServiceCIDR:      "10.247.128.0/17",
				VPCCloudCIDR:     "10.0.0.0/16",
				CloudSubnetCIDRs: map[string]string{"sub-1": "10.0.1.0/24"},
			},
			wantFields: []string{"containerNetwork.cidr"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// nil Validator => no cloud calls; pure checks only.
			var v *Validator
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
