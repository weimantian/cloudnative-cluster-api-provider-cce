/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
)

// eipFakeRoundTripper serves canned EIP API responses and records every
// request. Unlike fakeRoundTripper (adoption tests) it permits writes, so a
// test can prove createEip issued the cleanup delete.
type eipFakeRoundTripper struct {
	t        *testing.T
	routes   []fakeRoute
	requests []string
}

func (f *eipFakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req.Method+" "+req.URL.Path)
	for _, r := range f.routes {
		if r.method == req.Method && strings.Contains(req.URL.Path, r.pathSub) {
			status := r.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(r.body)),
				Request:    req,
			}, nil
		}
	}
	f.t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error_msg":"unexpected request"}`)),
		Request:    req,
	}, nil
}

func (f *eipFakeRoundTripper) sawRequest(method, pathSuffix string) bool {
	for _, r := range f.requests {
		if strings.HasPrefix(r, method+" ") && strings.HasSuffix(r, pathSuffix) {
			return true
		}
	}
	return false
}

// newEipTestManager builds a Manager whose EIP client talks to rt instead of
// Huawei Cloud (region/credentials resolve locally, no network I/O).
func newEipTestManager(t *testing.T, rt http.RoundTripper) *Manager {
	t.Helper()
	cred, err := basic.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithProjectId("project-test").SafeBuild()
	if err != nil {
		t.Fatalf("build fake credentials: %v", err)
	}
	httpConfig := config.DefaultHttpConfig().WithHttpRoundTripper(rt)
	eipRegion, err := eipregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve eip region: %v", err)
	}
	eipHC, err := eipv2.EipClientBuilder().WithRegion(eipRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build eip client: %v", err)
	}
	return &Manager{eip: eipv2.NewEipClient(eipHC)}
}

// TestCreateEipTagFailureDoesNotLeakUntaggedEip covers the anti-leak fix: an
// EIP that was created but could not be tagged must not be left allocated and
// invisible to the owned-tag GC sweep.
func TestCreateEipTagFailureDoesNotLeakUntaggedEip(t *testing.T) {
	ctx := context.Background()

	t.Run("tag failure deletes the untagged EIP", func(t *testing.T) {
		rt := &eipFakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodPost, pathSub: "/publicips/eip-1/tags", status: http.StatusInternalServerError, body: `{"error_msg":"tag boom"}`},
			{method: http.MethodGet, pathSub: "/publicips/eip-1", body: `{"publicip":{"id":"eip-1"}}`},
			{method: http.MethodDelete, pathSub: "/publicips/eip-1", body: `{}`},
			{method: http.MethodPost, pathSub: "/publicips", body: `{"publicip":{"id":"eip-1"}}`},
		}}

		id, err := newEipTestManager(t, rt).createEip(ctx, "demo-nat-eip", "demo")
		assertErrorContains(t, err, "CreatePublicipTag", "eip-1", "deleted")
		if id != "" {
			t.Errorf("createEip returned id %q after a successful cleanup delete, want empty", id)
		}
		if !rt.sawRequest(http.MethodDelete, "/publicips/eip-1") {
			t.Errorf("untagged EIP was not deleted; requests: %v", rt.requests)
		}
	})

	t.Run("cleanup delete failure returns the id for the caller", func(t *testing.T) {
		rt := &eipFakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodPost, pathSub: "/publicips/eip-1/tags", status: http.StatusInternalServerError, body: `{"error_msg":"tag boom"}`},
			{method: http.MethodGet, pathSub: "/publicips/eip-1", body: `{"publicip":{"id":"eip-1"}}`},
			{method: http.MethodDelete, pathSub: "/publicips/eip-1", status: http.StatusInternalServerError, body: `{"error_msg":"delete boom"}`},
			{method: http.MethodPost, pathSub: "/publicips", body: `{"publicip":{"id":"eip-1"}}`},
		}}

		id, err := newEipTestManager(t, rt).createEip(ctx, "demo-nat-eip", "demo")
		assertErrorContains(t, err, "CreatePublicipTag", "eip-1", "also failed")
		if id != "eip-1" {
			t.Errorf("createEip returned id %q, want eip-1 so the caller can persist it", id)
		}
	})
}
