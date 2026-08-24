/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package credentials resolves Huawei Cloud credentials for a reconcile. When
// an identity carries an agency, it assumes the agency via STS and returns
// temporary security credentials; otherwise it falls back to static AK/SK.
package credentials

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

// Credentials are the Huawei Cloud credentials used to build SDK clients for
// a single reconcile. When an agency is present, SecurityToken and ExpiresAt
// are populated from an STS AssumeAgency response (temporary credentials);
// otherwise only AccessKey and SecretKey are set (long-lived AK/SK).
type Credentials struct {
	AccessKey     string
	SecretKey     string
	SecurityToken string
	ExpiresAt     time.Time
}

// Provider assumes an agency and returns temporary security credentials.
type Provider interface {
	AssumeAgency(ctx context.Context, region, agencyName, accessKey, secretKey string) (*Credentials, error)
}

// Resolve returns static credentials when agencyName is empty, otherwise it
// delegates to the Provider to obtain temporary security credentials for the
// agency.
func Resolve(ctx context.Context, p Provider, region, agencyName, accessKey, secretKey string) (*Credentials, error) {
	if agencyName == "" {
		return &Credentials{AccessKey: accessKey, SecretKey: secretKey}, nil
	}
	if p == nil {
		return nil, errors.New("cannot assume agency with a nil provider")
	}
	return p.AssumeAgency(ctx, region, agencyName, accessKey, secretKey)
}

// NewProvider returns the STS-backed Provider.
func NewProvider() Provider {
	return &provider{
		getAccountID: getAccountID,
		assumeAgency: assumeAgency,
	}
}
