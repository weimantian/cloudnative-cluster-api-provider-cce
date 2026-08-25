/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command swr-login ensures the SWR organization (namespace) exists and prints
// a temporary docker login credential (username + password) derived from the
// AK/SK via the SWR CreateSecret API. The credential is short-lived (~1 hour);
// re-run to refresh.
//
// Env: CLOUD_SDK_AK / CLOUD_SDK_SK, CCE_DEPLOY_REGION (default cn-north-4),
// SWR_ORG (default capi_cce).
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	swrv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/swr/v2"
	swrmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/swr/v2/model"
	swrRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/swr/v2/region"
)

func main() {
	ak := envOr("CCE_DEPLOY_AK", "CLOUD_SDK_AK")
	sk := envOr("CCE_DEPLOY_SK", "CLOUD_SDK_SK")
	region := envDefault("CCE_DEPLOY_REGION", "cn-north-4")
	org := envDefault("SWR_ORG", "capi_cce")
	if ak == "" || sk == "" {
		fatal("CLOUD_SDK_AK and CLOUD_SDK_SK must be set")
	}

	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "credentials")
	r, err := swrRegion.SafeValueOf(region)
	must(err, "region")
	hc, err := swrv2.SwrClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "swr client")
	swr := swrv2.NewSwrClient(hc)

	// 1. Ensure the organization namespace exists.
	if err := ensureNamespace(swr, org); err != nil {
		fmt.Fprintf(os.Stderr, "note: ensure namespace: %v\n", err)
	} else {
		fmt.Printf("namespace %s ready\n", org)
	}

	// 2. Fetch a temporary docker login credential.
	auth, err := temporaryLogin(swr)
	must(err, "temporary login")
	fmt.Printf("SWR_REGISTRY=%s\n", registry(region))
	fmt.Printf("SWR_USER=%s\n", auth.user)
	fmt.Printf("SWR_PASSWORD=%s\n", auth.pass)
	fmt.Printf("SWR_ORG=%s\n", org)
	fmt.Printf("SWR_IMAGE=%s/%s/cluster-api-cce-controller:latest\n", registry(region), org)
}

type login struct{ user, pass string }

func temporaryLogin(swr *swrv2.SwrClient) (login, error) {
	ct := swrmodel.GetCreateSecretRequestContentTypeEnum().APPLICATION_JSON
	resp, err := swr.CreateSecret(&swrmodel.CreateSecretRequest{ContentType: ct})
	if err != nil {
		return login{}, err
	}
	if resp.Auths == nil {
		return login{}, fmt.Errorf("CreateSecret returned no auths")
	}
	for reg, a := range resp.Auths {
		decoded, err := base64.StdEncoding.DecodeString(a.Auth)
		if err != nil {
			continue
		}
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) == 2 {
			return login{user: parts[0], pass: parts[1]}, nil
		}
		_ = reg
	}
	return login{}, fmt.Errorf("no usable auth in CreateSecret response")
}

func ensureNamespace(swr *swrv2.SwrClient, ns string) error {
	ct := swrmodel.GetCreateNamespaceRequestContentTypeEnum().APPLICATION_JSON
	_, err := swr.CreateNamespace(&swrmodel.CreateNamespaceRequest{
		ContentType: ct,
		Body:        &swrmodel.CreateNamespaceRequestBody{Namespace: ns},
	})
	if err != nil && !strings.Contains(err.Error(), "already") && !strings.Contains(err.Error(), "SVCSTG.SWR.4000") {
		return err
	}
	return nil
}

func registry(region string) string { return "swr." + region + ".myhuaweicloud.com" }

func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func must(err error, what string) {
	if err != nil {
		fatal("%s: %v", what, err)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
