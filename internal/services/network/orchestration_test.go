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
	natv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2"
	natregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

type netRoute struct {
	method string
	sub    string
	body   string
}

// recordingNetRT serves canned responses for the VPC/NAT/EIP clients and
// records every request line so the teardown order can be asserted.
type recordingNetRT struct {
	t        *testing.T
	routes   []netRoute
	requests []string
}

func (r *recordingNetRT) RoundTrip(req *http.Request) (*http.Response, error) {
	target := req.URL.Path
	if req.URL.RawQuery != "" {
		target += "?" + req.URL.RawQuery
	}
	r.requests = append(r.requests, req.Method+" "+target)
	for _, rt := range r.routes {
		if rt.method == req.Method && strings.Contains(target, rt.sub) {
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

func (r *recordingNetRT) index(method, sub string) int {
	for i, q := range r.requests {
		if strings.HasPrefix(q, method+" ") && strings.Contains(q, sub) {
			return i
		}
	}
	return -1
}

// indexSuffix matches a request whose target ends with suffix, so that e.g.
// the SNAT-rule delete (…/nat_gateways/gw-1/snat_rules/rule-1) is not
// confused with the gateway delete (…/nat_gateways/gw-1).
func (r *recordingNetRT) indexSuffix(method, suffix string) int {
	for i, q := range r.requests {
		if strings.HasPrefix(q, method+" ") && strings.HasSuffix(q, suffix) {
			return i
		}
	}
	return -1
}

func testCreds(t *testing.T) *basic.Credentials {
	t.Helper()
	cred, err := basic.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithProjectId("project-test").SafeBuild()
	if err != nil {
		t.Fatalf("build credentials: %v", err)
	}
	return cred
}

func testManager(t *testing.T, rt http.RoundTripper) *Manager {
	t.Helper()
	cred := testCreds(t)
	httpConfig := config.DefaultHttpConfig().WithHttpRoundTripper(rt)

	vpcRegion, _ := vpcregion.SafeValueOf("cn-north-4")
	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(vpcRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build vpc client: %v", err)
	}
	natRegion, _ := natregion.SafeValueOf("cn-north-4")
	natHC, err := natv2.NatClientBuilder().WithRegion(natRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build nat client: %v", err)
	}
	eipRegion, _ := eipregion.SafeValueOf("cn-north-4")
	eipHC, err := eipv2.EipClientBuilder().WithRegion(eipRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build eip client: %v", err)
	}
	return &Manager{vpc: vpcv2.NewVpcClient(vpcHC), nat: natv2.NewNatClient(natHC), eip: eipv2.NewEipClient(eipHC)}
}

func TestDeleteNetworkTearsDownNatInOrder(t *testing.T) {
	rt := &recordingNetRT{t: t, routes: []netRoute{
		{method: http.MethodGet, sub: "/snat_rules", body: `{"snat_rules":[{"id":"rule-1","network_id":"subnet-1"}]}`},
		{method: http.MethodDelete, sub: "/snat_rules/rule-1", body: `{}`},
		{method: http.MethodGet, sub: "/nat_gateways/gw-1", body: `{"nat_gateway":{"id":"gw-1","status":"ACTIVE"}}`},
		{method: http.MethodDelete, sub: "/nat_gateways/gw-1", body: `{}`},
		{method: http.MethodGet, sub: "/publicips/eip-1", body: `{"publicip":{"id":"eip-1"}}`},
		{method: http.MethodDelete, sub: "/publicips/eip-1", body: `{}`},
		{method: http.MethodGet, sub: "/vpcs/vpc-1", body: `{"vpc":{"id":"vpc-1"}}`},
		{method: http.MethodDelete, sub: "/vpcs/vpc-1", body: `{}`},
	}}

	spec := &common.NetworkSpec{
		VPC:        common.VPC{ResourceID: "vpc-1"},
		NatGateway: &common.NatGatewaySpec{ResourceID: "gw-1", EIPResourceID: "eip-1"},
	}
	if err := testManager(t, rt).DeleteNetwork(context.Background(), spec, "demo"); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}

	rules := rt.indexSuffix(http.MethodDelete, "/snat_rules/rule-1")
	gateway := rt.indexSuffix(http.MethodDelete, "/nat_gateways/gw-1")
	eip := rt.indexSuffix(http.MethodDelete, "/publicips/eip-1")
	vpc := rt.indexSuffix(http.MethodDelete, "/vpcs/vpc-1")
	if rules < 0 || gateway < 0 || eip < 0 || vpc < 0 {
		t.Fatalf("missing teardown call, got %v", rt.requests)
	}
	if !(rules < gateway) {
		t.Errorf("SNAT rules must be deleted before the NAT gateway, got %v", rt.requests)
	}
	if !(gateway < eip) {
		t.Errorf("the NAT gateway must be deleted before its EIP is released, got %v", rt.requests)
	}
	if !(eip < vpc) {
		t.Errorf("the VPC must be deleted last, got %v", rt.requests)
	}
}

func TestDeleteNetworkLeavesBYOAlone(t *testing.T) {
	// vpc.id set without the owned tag => BYO: no cloud call at all.
	rt := &recordingNetRT{t: t}
	spec := &common.NetworkSpec{VPC: common.VPC{ID: "byo-vpc"}}
	if err := testManager(t, rt).DeleteNetwork(context.Background(), spec, "demo"); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}
	if len(rt.requests) != 0 {
		t.Errorf("a BYO network must not be touched, got %v", rt.requests)
	}
}

func TestEnsureSnatRules(t *testing.T) {
	ctx := context.Background()

	managedSubnet := common.Subnet{Type: common.SubnetTypeNode, ResourceID: "subnet-1"}
	ng := &common.NatGatewaySpec{ResourceID: "gw-1", EIPResourceID: "eip-1"}

	t.Run("creates a rule for a managed node subnet without one", func(t *testing.T) {
		rt := &recordingNetRT{t: t, routes: []netRoute{
			{method: http.MethodGet, sub: "/snat_rules", body: `{"snat_rules":[]}`},
			{method: http.MethodPost, sub: "/snat_rules", body: `{}`},
		}}
		spec := &common.NetworkSpec{Subnets: []common.Subnet{managedSubnet}}
		if err := testManager(t, rt).ensureSnatRules(ctx, spec, ng); err != nil {
			t.Fatalf("ensureSnatRules: %v", err)
		}
		if rt.index(http.MethodPost, "/snat_rules") < 0 {
			t.Errorf("expected a SNAT-rule create, got %v", rt.requests)
		}
	})

	t.Run("skips a subnet that already has a rule", func(t *testing.T) {
		rt := &recordingNetRT{t: t, routes: []netRoute{
			{method: http.MethodGet, sub: "/snat_rules", body: `{"snat_rules":[{"id":"rule-1","network_id":"subnet-1"}]}`},
		}}
		spec := &common.NetworkSpec{Subnets: []common.Subnet{managedSubnet}}
		if err := testManager(t, rt).ensureSnatRules(ctx, spec, ng); err != nil {
			t.Fatalf("ensureSnatRules: %v", err)
		}
		if rt.index(http.MethodPost, "/snat_rules") >= 0 {
			t.Errorf("expected no SNAT-rule create for an existing rule, got %v", rt.requests)
		}
	})
}
