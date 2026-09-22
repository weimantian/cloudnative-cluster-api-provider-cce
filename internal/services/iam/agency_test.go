/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package iam

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/global"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	iamv5 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/iam/v5"
	iamregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/iam/v5/region"
)

type iamRoute struct {
	method  string
	pathSub string
	body    string
}

// recordingIAMRT serves canned JSON by method+path and records every request,
// mirroring the transport harness in internal/hwsdk/sdk_test.go.
type recordingIAMRT struct {
	t        *testing.T
	routes   []iamRoute
	requests []string
}

func requestTarget(req *http.Request) string {
	if req.URL.RawQuery == "" {
		return req.URL.Path
	}
	return req.URL.Path + "?" + req.URL.RawQuery
}

func (r *recordingIAMRT) RoundTrip(req *http.Request) (*http.Response, error) {
	target := requestTarget(req)
	r.requests = append(r.requests, req.Method+" "+target)
	for _, rt := range r.routes {
		if rt.method == req.Method && strings.Contains(target, rt.pathSub) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(rt.body)),
				Request:    req,
			}, nil
		}
	}
	r.t.Errorf("unexpected request %s %s", req.Method, target)
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error_msg":"unexpected request"}`)),
		Request:    req,
	}, nil
}

func (r *recordingIAMRT) count(method, pathSub string) int {
	n := 0
	for _, q := range r.requests {
		if strings.HasPrefix(q, method+" ") && strings.Contains(q, pathSub) {
			n++
		}
	}
	return n
}

func newTestIAMClient(t *testing.T, rt http.RoundTripper) *iamv5.IamClient {
	t.Helper()
	cred, err := global.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithDomainId("domain-test").SafeBuild()
	if err != nil {
		t.Fatalf("build credentials: %v", err)
	}
	region, err := iamregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve iam region: %v", err)
	}
	hc, err := iamv5.IamClientBuilder().
		WithRegion(region).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig().WithHttpRoundTripper(rt)).
		SafeBuild()
	if err != nil {
		t.Fatalf("build iam client: %v", err)
	}
	return iamv5.NewIamClient(hc)
}

func TestAgencyExists(t *testing.T) {
	t.Run("match on first page", func(t *testing.T) {
		rt := &recordingIAMRT{t: t, routes: []iamRoute{
			{method: http.MethodGet, pathSub: "/agencies", body: `{"agencies":[{"agency_name":"other"},{"agency_name":"target"}]}`},
		}}
		c := &Client{iam: newTestIAMClient(t, rt)}
		got, err := c.agencyExists(context.Background(), "target")
		if err != nil {
			t.Fatalf("agencyExists: %v", err)
		}
		if !got {
			t.Error("expected agencyExists=true when the name is present")
		}
	})

	t.Run("match on second page (marker pagination)", func(t *testing.T) {
		// First page has a NextMarker but no match; the loop must follow it.
		rt := &recordingIAMRT{t: t, routes: []iamRoute{
			{method: http.MethodGet, pathSub: "marker=page2", body: `{"agencies":[{"agency_name":"target"}]}`},
			{method: http.MethodGet, pathSub: "/agencies", body: `{"agencies":[{"agency_name":"other"}],"page_info":{"next_marker":"page2"}}`},
		}}
		c := &Client{iam: newTestIAMClient(t, rt)}
		got, err := c.agencyExists(context.Background(), "target")
		if err != nil {
			t.Fatalf("agencyExists: %v", err)
		}
		if !got {
			t.Error("expected agencyExists=true after following NextMarker")
		}
		if n := rt.count(http.MethodGet, "/agencies"); n != 2 {
			t.Errorf("expected 2 list pages, got %d", n)
		}
	})

	t.Run("absent", func(t *testing.T) {
		rt := &recordingIAMRT{t: t, routes: []iamRoute{
			{method: http.MethodGet, pathSub: "/agencies", body: `{"agencies":[{"agency_name":"other"}]}`},
		}}
		c := &Client{iam: newTestIAMClient(t, rt)}
		got, err := c.agencyExists(context.Background(), "target")
		if err != nil {
			t.Fatalf("agencyExists: %v", err)
		}
		if got {
			t.Error("expected agencyExists=false when the name is absent")
		}
	})
}

func TestEnsureAgencyCreatesOnlyWhenAbsent(t *testing.T) {
	ctx := context.Background()

	t.Run("existing agency is adopted, not recreated", func(t *testing.T) {
		rt := &recordingIAMRT{t: t, routes: []iamRoute{
			{method: http.MethodGet, pathSub: "/agencies", body: `{"agencies":[{"agency_name":"target"}]}`},
		}}
		c := &Client{iam: newTestIAMClient(t, rt)}
		if err := c.EnsureAgency(ctx, "target", `{"Version":"5.0"}`); err != nil {
			t.Fatalf("EnsureAgency: %v", err)
		}
		if n := rt.count(http.MethodPost, "/agencies"); n != 0 {
			t.Errorf("expected no CreateAgencyV5 call for an existing agency, got %d", n)
		}
	})

	t.Run("missing agency is created", func(t *testing.T) {
		rt := &recordingIAMRT{t: t, routes: []iamRoute{
			{method: http.MethodGet, pathSub: "/agencies", body: `{"agencies":[]}`},
			{method: http.MethodPost, pathSub: "/agencies", body: `{"agency":{"agency_name":"target"}}`},
		}}
		c := &Client{iam: newTestIAMClient(t, rt)}
		if err := c.EnsureAgency(ctx, "target", `{"Version":"5.0"}`); err != nil {
			t.Fatalf("EnsureAgency: %v", err)
		}
		if n := rt.count(http.MethodPost, "/agencies"); n != 1 {
			t.Errorf("expected exactly 1 CreateAgencyV5 call, got %d", n)
		}
	})
}
