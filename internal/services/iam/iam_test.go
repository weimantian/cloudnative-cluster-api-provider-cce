/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package iam

import "testing"

func TestValidateTrustPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		wantErr bool
	}{
		{
			name: "valid v5 policy",
			policy: `{
				"Version": "5.0",
				"Statement": [{
					"Effect": "Allow",
					"Principal": {"Service": ["cce"]},
					"Action": ["sts:agencies:assume"]
				}]
			}`,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			policy:  `{not json`,
			wantErr: true,
		},
		{
			name:    "missing version",
			policy:  `{"Statement": []}`,
			wantErr: true,
		},
		{
			name:    "wrong version",
			policy:  `{"Version": "1.0", "Statement": []}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTrustPolicy(tt.policy)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTrustPolicy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
