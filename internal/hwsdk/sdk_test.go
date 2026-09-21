/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package hwsdk

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	natv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2"
	natregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/region"
)

type natRoute struct {
	method  string
	pathSub string
	status  int
	body    string
}

type recordingNatRT struct {
	t        *testing.T
	requests []string
	routes   []natRoute
}

func (r *recordingNatRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, req.Method+" "+req.URL.Path)
	for _, rt := range r.routes {
		if rt.method == req.Method && strings.Contains(req.URL.Path, rt.pathSub) {
			status := rt.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(rt.body)),
				Request:    req,
			}, nil
		}
	}
	r.t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error_msg":"unexpected request"}`)),
		Request:    req,
	}, nil
}

func newTestNatClient(t *testing.T, rt http.RoundTripper) *natv2.NatClient {
	t.Helper()
	cred, err := basic.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithProjectId("project-test").SafeBuild()
	if err != nil {
		t.Fatalf("build credentials: %v", err)
	}
	region, err := natregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve nat region: %v", err)
	}
	hc, err := natv2.NatClientBuilder().
		WithRegion(region).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig().WithHttpRoundTripper(rt)).
		SafeBuild()
	if err != nil {
		t.Fatalf("build nat client: %v", err)
	}
	return natv2.NewNatClient(hc)
}

func indexOfRequest(requests []string, method, suffix string) int {
	for i, r := range requests {
		if strings.HasPrefix(r, method+" ") && strings.HasSuffix(r, suffix) {
			return i
		}
	}
	return -1
}

// TestDeleteNatGatewayOrderedDeletesSnatRulesFirst pins the platform invariant:
// a gateway's SNAT rules are deleted before the gateway itself.
func TestDeleteNatGatewayOrderedDeletesSnatRulesFirst(t *testing.T) {
	rt := &recordingNatRT{t: t, routes: []natRoute{
		{method: http.MethodGet, pathSub: "/snat_rules", body: `{"snat_rules":[{"id":"rule-1"}]}`},
		{method: http.MethodDelete, pathSub: "/snat_rules/rule-1", body: `{}`},
		{method: http.MethodDelete, pathSub: "/nat_gateways/gw-1", body: `{}`},
	}}
	nat := newTestNatClient(t, rt)

	rulesErr, gatewayErr := DeleteNatGatewayOrdered(
		func() error {
			_, _, ruleErr := DeleteSnatRules(nat, "gw-1")
			return ruleErr
		},
		func() error { return DeleteNatGateway(nat, "gw-1") },
		true,
	)
	if rulesErr != nil || gatewayErr != nil {
		t.Fatalf("teardown errors: rules=%v gateway=%v", rulesErr, gatewayErr)
	}

	list := indexOfRequest(rt.requests, http.MethodGet, "/snat_rules")
	rule := indexOfRequest(rt.requests, http.MethodDelete, "/snat_rules/rule-1")
	gateway := indexOfRequest(rt.requests, http.MethodDelete, "/nat_gateways/gw-1")
	if list < 0 || rule < 0 || gateway < 0 {
		t.Fatalf("missing teardown call; requests: %v", rt.requests)
	}
	if !(list < rule && rule < gateway) {
		t.Fatalf("SNAT rules must be deleted before the gateway; requests: %v", rt.requests)
	}
}

// TestDeleteNatGatewayOrderedFailFast covers the failFast contract the CCE GC
// relies on: once rules deletion fails the gateway delete is not attempted.
func TestDeleteNatGatewayOrderedFailFast(t *testing.T) {
	var calls []string
	rulesErr, gatewayErr := DeleteNatGatewayOrdered(
		func() error { calls = append(calls, "rules"); return errors.New("rules boom") },
		func() error { calls = append(calls, "gateway"); return nil },
		true,
	)
	if rulesErr == nil || gatewayErr != nil || len(calls) != 1 || calls[0] != "rules" {
		t.Fatalf("failFast=true: calls=%v rulesErr=%v gatewayErr=%v", calls, rulesErr, gatewayErr)
	}

	calls = nil
	_, gatewayErr = DeleteNatGatewayOrdered(
		func() error { calls = append(calls, "rules"); return errors.New("rules boom") },
		func() error { calls = append(calls, "gateway"); return errors.New("gateway boom") },
		false,
	)
	if gatewayErr == nil || len(calls) != 2 || calls[0] != "rules" || calls[1] != "gateway" {
		t.Fatalf("failFast=false: calls=%v gatewayErr=%v", calls, gatewayErr)
	}
}
