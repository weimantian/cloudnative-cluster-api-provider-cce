/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package credentials

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	sts "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/sts/v1"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/sts/v1/model"
	stsregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/sts/v1/region"
	"github.com/pkg/errors"
)

// agencySessionName is the session name used for the AssumeAgency call. It is
// stable across reconciles so temporary credentials are traceable.
const agencySessionName = "cce-cluster-api"

// provider is the STS-backed implementation of Provider. The getAccountID and
// assumeAgency fields are function seams, overridable in tests.
type provider struct {
	getAccountID func(ctx context.Context, region, accessKey, secretKey string) (string, error)
	assumeAgency func(ctx context.Context, region, agencyURN, accessKey, secretKey string) (*Credentials, error)
}

// AssumeAgency resolves the caller's account ID, derives the agency URN, and
// assumes the agency via STS.
func (p *provider) AssumeAgency(ctx context.Context, region, agencyName, accessKey, secretKey string) (*Credentials, error) {
	accountID, err := p.getAccountID(ctx, region, accessKey, secretKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve account ID for agency assumption")
	}
	agencyURN := agencyName
	if !strings.HasPrefix(agencyName, "urn:") {
		agencyURN = fmt.Sprintf("urn:stia:%s:agency:%s", accountID, agencyName)
	}
	creds, err := p.assumeAgency(ctx, region, agencyURN, accessKey, secretKey)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to assume agency %q", agencyName)
	}
	return creds, nil
}

// getAccountID resolves the caller's Huawei Cloud account ID via STS
// GetCallerIdentity.
func getAccountID(ctx context.Context, region, accessKey, secretKey string) (string, error) {
	reg, err := stsregion.SafeValueOf(region)
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve STS region %q", region)
	}
	cred, err := basic.NewCredentialsBuilder().
		WithAk(accessKey).
		WithSk(secretKey).
		SafeBuild()
	if err != nil {
		return "", errors.Wrap(err, "failed to build STS credentials")
	}
	hcClient, err := sts.StsClientBuilder().
		WithRegion(reg).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).
		SafeBuild()
	if err != nil {
		return "", errors.Wrap(err, "failed to build STS client")
	}
	resp, err := sts.NewStsClient(hcClient).GetCallerIdentity(&model.GetCallerIdentityRequest{})
	if err != nil {
		return "", errors.Wrap(err, "GetCallerIdentity failed")
	}
	if resp.AccountId == nil || *resp.AccountId == "" {
		return "", errors.New("GetCallerIdentity returned an empty account ID")
	}
	return *resp.AccountId, nil
}

// assumeAgency calls STS AssumeAgency and converts the response to
// Credentials.
func assumeAgency(ctx context.Context, region, agencyURN, accessKey, secretKey string) (*Credentials, error) {
	reg, err := stsregion.SafeValueOf(region)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve STS region %q", region)
	}
	cred, err := basic.NewCredentialsBuilder().
		WithAk(accessKey).
		WithSk(secretKey).
		SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build STS credentials")
	}
	hcClient, err := sts.StsClientBuilder().
		WithRegion(reg).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).
		SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build STS client")
	}
	duration := int32(3600)
	resp, err := sts.NewStsClient(hcClient).AssumeAgency(&model.AssumeAgencyRequest{
		Body: &model.AssumeAgencyReqBody{
			AgencyUrn:         agencyURN,
			AgencySessionName: agencySessionName,
			DurationSeconds:   &duration,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "AssumeAgency failed")
	}
	if resp.Credentials == nil {
		return nil, errors.New("AssumeAgency returned no credentials")
	}
	creds := &Credentials{
		AccessKey:     resp.Credentials.AccessKeyId,
		SecretKey:     resp.Credentials.SecretAccessKey,
		SecurityToken: resp.Credentials.SecurityToken,
	}
	if resp.Credentials.Expiration != nil {
		creds.ExpiresAt = time.Time(*resp.Credentials.Expiration)
	}
	return creds, nil
}
