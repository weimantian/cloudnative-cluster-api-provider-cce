/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package credentials

import (
	"context"
	"testing"
)

func TestResolveStaticCredentials(t *testing.T) {
	creds, err := Resolve(context.Background(), nil, "cn-north-4", "", "ak", "sk")
	if err != nil {
		t.Fatalf("Resolve (static) failed: %v", err)
	}
	if creds.AccessKey != "ak" || creds.SecretKey != "sk" {
		t.Errorf("Resolve (static) = %+v, want ak/sk", creds)
	}
	if creds.SecurityToken != "" {
		t.Errorf("Resolve (static) SecurityToken = %q, want empty", creds.SecurityToken)
	}
}

func TestResolveAgencyDelegates(t *testing.T) {
	expected := &Credentials{AccessKey: "tmp-ak", SecretKey: "tmp-sk", SecurityToken: "token"}
	fake := &provider{
		getAccountID: func(ctx context.Context, region, accessKey, secretKey string) (string, error) {
			return "acct-123", nil
		},
		assumeAgency: func(ctx context.Context, region, agencyURN, accessKey, secretKey string) (*Credentials, error) {
			if agencyURN != "urn:stia:acct-123:agency:my-agency" {
				t.Errorf("agencyURN = %q, want derived URN", agencyURN)
			}
			return expected, nil
		},
	}
	creds, err := Resolve(context.Background(), fake, "cn-north-4", "my-agency", "ak", "sk")
	if err != nil {
		t.Fatalf("Resolve (agency) failed: %v", err)
	}
	if creds != expected {
		t.Errorf("Resolve (agency) = %+v, want %+v", creds, expected)
	}
}

func TestResolveAgencyNilProvider(t *testing.T) {
	if _, err := Resolve(context.Background(), nil, "cn-north-4", "my-agency", "ak", "sk"); err == nil {
		t.Fatal("Resolve with agency and nil provider should error")
	}
}

func TestAssumeAgencyKeepsFullURN(t *testing.T) {
	fake := &provider{
		getAccountID: func(ctx context.Context, region, accessKey, secretKey string) (string, error) {
			return "acct-123", nil
		},
		assumeAgency: func(ctx context.Context, region, agencyURN, accessKey, secretKey string) (*Credentials, error) {
			if agencyURN != "urn:stia:acct-999:agency:other" {
				t.Errorf("agencyURN = %q, want pass-through URN", agencyURN)
			}
			return &Credentials{}, nil
		},
	}
	if _, err := fake.AssumeAgency(context.Background(), "cn-north-4", "urn:stia:acct-999:agency:other", "ak", "sk"); err != nil {
		t.Fatalf("AssumeAgency (full URN) failed: %v", err)
	}
}
