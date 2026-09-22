/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package common

import "testing"

func TestParseCIDR(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
		wantStr string
	}{
		{name: "ipv4", in: "10.0.0.0/16", wantStr: "10.0.0.0/16"},
		{name: "ipv4 host bits", in: "10.0.1.5/24", wantStr: "10.0.1.0/24"}, // ParsePrefix keeps the prefix but Masked() reveals network; ParsePrefix returns unmasked
		{name: "ipv6", in: "fd00::/64", wantStr: "fd00::/64"},
		{name: "empty", in: "", wantErr: true},
		{name: "no mask", in: "10.0.0.0", wantErr: true},
		{name: "invalid octet", in: "300.0.0.0/8", wantErr: true},
		{name: "garbage", in: "not-a-cidr", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseCIDR(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseCIDR(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got := p.Masked().String(); got != tc.wantStr {
				t.Errorf("ParseCIDR(%q).Masked() = %q, want %q", tc.in, got, tc.wantStr)
			}
		})
	}
}
