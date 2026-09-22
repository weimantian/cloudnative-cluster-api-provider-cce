/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package hwutil holds helpers shared by the one-off operations tools under
// hack/, so duplicated functions (env lookup, 429 retry, EIP creation and
// EIP client construction) each have a single implementation.
package hwutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
)

// EnvOr returns the first non-empty value among the given environment
// variable keys, or "" if none are set.
func EnvOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// RetryThrottled retries fn when the Huawei Cloud API reports 429
// (APIGW.0308 throttling). Each retry sleeps to let the per-minute write
// window drain before trying again, so repeated attempts do not keep
// refreshing the counter (verified: retries count towards the limit).
func RetryThrottled(desc string, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err = fn()
		if !IsThrottled(err) {
			return err
		}
		wait := time.Duration(60*(attempt+1)) * time.Second
		fmt.Printf("%s: throttled (429), retrying in %v (attempt %d/%d)\n", desc, wait, attempt+1, maxRetries)
		time.Sleep(wait)
	}
	return err
}

// IsThrottled reports whether err is a Huawei Cloud 429 (APIGW.0308) error.
func IsThrottled(err error) bool {
	if err == nil {
		return false
	}
	var se *sdkerr.ServiceResponseError
	if errors.As(err, &se) {
		return se.StatusCode == 429
	}
	return false
}

// NewEipClient builds a Huawei Cloud EIP v2 client from AK/SK credentials.
func NewEipClient(region, ak, sk string) (*eipv2.EipClient, error) {
	r, err := eipregion.SafeValueOf(region)
	if err != nil {
		return nil, err
	}
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		return nil, err
	}
	hc, err := eipv2.EipClientBuilder().WithRegion(r).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return nil, err
	}
	return eipv2.NewEipClient(hc), nil
}

// CreatePublicIP creates a dedicated "5_bgp" public EIP with a 5 Mbps
// bandwidth, returning the EIP id and public address.
func CreatePublicIP(ctx context.Context, c *eipv2.EipClient, name string) (string, string, error) {
	shareType := eipmodel.GetCreatePublicipBandwidthOptionShareTypeEnum().PER
	bandwidth := eipmodel.CreatePublicipBandwidthOption{ShareType: shareType, Name: &name, Size: int32Ptr(5)}
	publicip := eipmodel.CreatePublicipOption{Type: "5_bgp", Alias: &name}
	resp, err := c.CreatePublicip(&eipmodel.CreatePublicipRequest{Body: &eipmodel.CreatePublicipRequestBody{
		Bandwidth: &bandwidth,
		Publicip:  &publicip,
	}})
	if err != nil {
		return "", "", err
	}
	id, addr := "", ""
	if resp.Publicip != nil {
		if resp.Publicip.Id != nil {
			id = *resp.Publicip.Id
		}
		if resp.Publicip.PublicIpAddress != nil {
			addr = *resp.Publicip.PublicIpAddress
		}
	}
	return id, addr, nil
}

func int32Ptr(v int32) *int32 { return &v }
