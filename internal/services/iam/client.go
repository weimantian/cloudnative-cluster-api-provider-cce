/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package iam

import (
	"context"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/global"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	iamv5 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/iam/v5"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/iam/v5/model"
	iamregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/iam/v5/region"
	"github.com/pkg/errors"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
)

// Client is the IAM v5 SDK-backed implementation of Service.
type Client struct {
	iam *iamv5.IamClient
}

var _ Service = (*Client)(nil)

// NewClient builds an IAM v5 client for the given region and credentials.
// IAM is a global service, so the client uses global credentials
// (global.Credentials) rather than the per-region basic.Credentials used by
// the CCE/VPC/NAT clients. The trust-agency APIs (ListAgenciesV5 /
// CreateAgencyV5) live on the v5 surface.
func NewClient(regionID string, creds *credentials.Credentials) (*Client, error) {
	region, err := iamregion.SafeValueOf(regionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve IAM region %q", regionID)
	}
	builder := global.NewCredentialsBuilder().WithAk(creds.AccessKey).WithSk(creds.SecretKey)
	if creds.SecurityToken != "" {
		builder = builder.WithSecurityToken(creds.SecurityToken)
	}
	cred, err := builder.SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build IAM credentials")
	}
	hcClient, err := iamv5.IamClientBuilder().
		WithRegion(region).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).
		SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build IAM HTTP client")
	}
	return &Client{iam: iamv5.NewIamClient(hcClient)}, nil
}

// EnsureAgency implements Service. It lists the account's agencies (paginating
// via PageInfo.NextMarker) and creates the trust agency only when the name is
// absent. An existing agency — whether it carries the same trust policy or a
// different one — is adopted untouched, so the provider never overwrites a
// user-managed delegation.
func (c *Client) EnsureAgency(ctx context.Context, agencyName, trustPolicy string) error {
	exists, err := c.agencyExists(ctx, agencyName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = c.iam.CreateAgencyV5(&model.CreateAgencyV5Request{
		Body: &model.CreateAgencyReqBody{
			AgencyName:  agencyName,
			TrustPolicy: trustPolicy,
		},
	})
	if err != nil {
		return errors.Wrapf(err, "CreateAgencyV5 %q failed", agencyName)
	}
	return nil
}

// agencyExists reports whether an agency with the given name already exists in
// the account. ListAgenciesV5 pages via PageInfo.NextMarker (marker-based), so
// this loops until the page marker is exhausted.
func (c *Client) agencyExists(ctx context.Context, agencyName string) (bool, error) {
	var marker *string
	for {
		limit := int32(200)
		resp, err := c.iam.ListAgenciesV5(&model.ListAgenciesV5Request{Limit: &limit, Marker: marker})
		if err != nil {
			return false, errors.Wrap(err, "ListAgenciesV5 failed")
		}
		if resp.Agencies != nil {
			for _, a := range *resp.Agencies {
				if a.AgencyName == agencyName {
					return true, nil
				}
			}
		}
		if resp.PageInfo == nil || resp.PageInfo.NextMarker == nil {
			return false, nil
		}
		marker = resp.PageInfo.NextMarker
	}
}
