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

const (
	ownedTagsJSON    = `{"tags":[{"key":"cluster-api-provider-cce.cluster.demo","value":"owned"}]}`
	notOwnedTagsJSON = `{"tags":[{"key":"team","value":"platform"}]}`
)

// fakeRoute is one canned HTTP response keyed on method + a path substring.
type fakeRoute struct {
	method  string
	pathSub string
	status  int // 0 => 200
	body    string
}

// fakeRoundTripper serves canned cloud responses without contacting Huawei
// Cloud, and records every write (non-GET) request so a test can prove that
// adoption/conflict never created or deleted anything.
type fakeRoundTripper struct {
	t      *testing.T
	routes []fakeRoute
	writes []string
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		f.writes = append(f.writes, req.Method+" "+req.URL.Path)
		f.t.Errorf("unexpected write request %s %s", req.Method, req.URL.Path)
	}
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

// newAdoptionTestManager builds a Manager whose SDK clients talk to rt instead
// of Huawei Cloud (region/credentials are resolved locally, no network I/O).
func newAdoptionTestManager(t *testing.T, rt http.RoundTripper) *Manager {
	t.Helper()
	cred, err := basic.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithProjectId("project-test").SafeBuild()
	if err != nil {
		t.Fatalf("build fake credentials: %v", err)
	}
	httpConfig := config.DefaultHttpConfig().WithHttpRoundTripper(rt)

	vpcRegion, err := vpcregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve vpc region: %v", err)
	}
	natRegion, err := natregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve nat region: %v", err)
	}
	eipRegion, err := eipregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve eip region: %v", err)
	}

	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(vpcRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build vpc client: %v", err)
	}
	natHC, err := natv2.NatClientBuilder().WithRegion(natRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build nat client: %v", err)
	}
	eipHC, err := eipv2.EipClientBuilder().WithRegion(eipRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		t.Fatalf("build eip client: %v", err)
	}
	return &Manager{vpc: vpcv2.NewVpcClient(vpcHC), nat: natv2.NewNatClient(natHC), eip: eipv2.NewEipClient(eipHC)}
}

func assertErrorContains(t *testing.T, err error, substrs ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %v, got nil", substrs)
	}
	for _, s := range substrs {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not contain %q", err.Error(), s)
		}
	}
}

// assertConflict asserts the explicit adoption-conflict error shape.
func assertConflict(t *testing.T, err error, resourceType, name, id string) {
	t.Helper()
	assertErrorContains(t, err, resourceType, name, id, "not owned by this provider", "refusing to adopt")
}

func assertNoWrites(t *testing.T, rt *fakeRoundTripper) {
	t.Helper()
	if len(rt.writes) != 0 {
		t.Errorf("expected no create/delete calls, got %v", rt.writes)
	}
}

// TestNameAdoptionRequiresOwnedTag is the core safety test: a name-matched
// resource is adopted only when it carries the provider owned tag; anything
// else yields an explicit conflict error and triggers no create/delete call.
func TestNameAdoptionRequiresOwnedTag(t *testing.T) {
	ctx := context.Background()

	t.Run("vpc adopted when owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/vpcs/vpc-1/tags", body: ownedTagsJSON},
			{method: http.MethodGet, pathSub: "/vpcs", body: `{"vpcs":[{"id":"vpc-1","name":"demo-vpc"}]}`},
		}}
		spec := &common.NetworkSpec{VPC: common.VPC{Name: "demo-vpc"}}
		if err := newAdoptionTestManager(t, rt).ensureVpc(ctx, spec, "demo"); err != nil {
			t.Fatalf("ensureVpc: %v", err)
		}
		if spec.VPC.ResourceID != "vpc-1" {
			t.Errorf("ResourceID = %q, want vpc-1", spec.VPC.ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("vpc conflict when not owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/vpcs/vpc-1/tags", body: notOwnedTagsJSON},
			{method: http.MethodGet, pathSub: "/vpcs", body: `{"vpcs":[{"id":"vpc-1","name":"demo-vpc"}]}`},
		}}
		spec := &common.NetworkSpec{VPC: common.VPC{Name: "demo-vpc"}}
		err := newAdoptionTestManager(t, rt).ensureVpc(ctx, spec, "demo")
		assertConflict(t, err, "VPC", "demo-vpc", "vpc-1")
		if spec.VPC.ResourceID != "" {
			t.Errorf("ResourceID = %q, want empty (must not adopt)", spec.VPC.ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("vpc conflict when tags unreadable", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/vpcs/vpc-1/tags", status: http.StatusInternalServerError, body: `{"error_msg":"boom"}`},
			{method: http.MethodGet, pathSub: "/vpcs", body: `{"vpcs":[{"id":"vpc-1","name":"demo-vpc"}]}`},
		}}
		spec := &common.NetworkSpec{VPC: common.VPC{Name: "demo-vpc"}}
		err := newAdoptionTestManager(t, rt).ensureVpc(ctx, spec, "demo")
		assertErrorContains(t, err, "VPC", "demo-vpc", "vpc-1", "could not be read", "refusing to adopt")
		if spec.VPC.ResourceID != "" {
			t.Errorf("ResourceID = %q, want empty (fail closed)", spec.VPC.ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("subnet adopted when owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/subnets/sub-1/tags", body: ownedTagsJSON},
			{method: http.MethodGet, pathSub: "/subnets", body: `{"subnets":[{"id":"sub-1","name":"node-1","neutron_subnet_id":"neu-1"}]}`},
		}}
		spec := &common.NetworkSpec{
			VPC:     common.VPC{ResourceID: "vpc-1"},
			Subnets: []common.Subnet{{Name: "node-1", CIDR: "10.0.1.0/24"}},
		}
		if err := newAdoptionTestManager(t, rt).ensureSubnets(ctx, spec, "demo"); err != nil {
			t.Fatalf("ensureSubnets: %v", err)
		}
		if spec.Subnets[0].ResourceID != "sub-1" || spec.Subnets[0].NeutronSubnetID != "neu-1" {
			t.Errorf("subnet = %+v, want ResourceID sub-1 / NeutronSubnetID neu-1", spec.Subnets[0])
		}
		assertNoWrites(t, rt)
	})

	t.Run("subnet conflict when not owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/subnets/sub-1/tags", body: notOwnedTagsJSON},
			{method: http.MethodGet, pathSub: "/subnets", body: `{"subnets":[{"id":"sub-1","name":"node-1","neutron_subnet_id":"neu-1"}]}`},
		}}
		spec := &common.NetworkSpec{
			VPC:     common.VPC{ResourceID: "vpc-1"},
			Subnets: []common.Subnet{{Name: "node-1", CIDR: "10.0.1.0/24"}},
		}
		err := newAdoptionTestManager(t, rt).ensureSubnets(ctx, spec, "demo")
		assertConflict(t, err, "subnet", "node-1", "sub-1")
		if spec.Subnets[0].ResourceID != "" {
			t.Errorf("ResourceID = %q, want empty (must not adopt)", spec.Subnets[0].ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("security group adopted when owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/security-groups/sg-1/tags", body: ownedTagsJSON},
			{method: http.MethodGet, pathSub: "/security-groups", body: `{"security_groups":[{"id":"sg-1","name":"demo-node"}]}`},
			{method: http.MethodGet, pathSub: "/security-group-rules", body: `{"security_group_rules":[]}`},
		}}
		spec := &common.NetworkSpec{
			VPC:           common.VPC{ResourceID: "vpc-1"},
			SecurityGroup: &common.SecurityGroupSpec{Name: "demo-node"},
		}
		if err := newAdoptionTestManager(t, rt).ensureSecurityGroup(ctx, spec, "demo"); err != nil {
			t.Fatalf("ensureSecurityGroup: %v", err)
		}
		if spec.SecurityGroup.ResourceID != "sg-1" {
			t.Errorf("ResourceID = %q, want sg-1", spec.SecurityGroup.ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("security group conflict when not owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/security-groups/sg-1/tags", body: notOwnedTagsJSON},
			{method: http.MethodGet, pathSub: "/security-groups", body: `{"security_groups":[{"id":"sg-1","name":"demo-node"}]}`},
		}}
		spec := &common.NetworkSpec{
			VPC:           common.VPC{ResourceID: "vpc-1"},
			SecurityGroup: &common.SecurityGroupSpec{Name: "demo-node"},
		}
		err := newAdoptionTestManager(t, rt).ensureSecurityGroup(ctx, spec, "demo")
		assertConflict(t, err, "security group", "demo-node", "sg-1")
		if spec.SecurityGroup.ResourceID != "" {
			t.Errorf("ResourceID = %q, want empty (must not adopt)", spec.SecurityGroup.ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("nat gateway adopted when owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/nat_gateways/nat-1/tags", body: ownedTagsJSON},
			{method: http.MethodGet, pathSub: "/nat_gateways/nat-1", body: `{"nat_gateway":{"id":"nat-1","name":"demo-nat","status":"ACTIVE"}}`},
			{method: http.MethodGet, pathSub: "/nat_gateways", body: `{"nat_gateways":[{"id":"nat-1","name":"demo-nat"}]}`},
			{method: http.MethodGet, pathSub: "/snat_rules", body: `{"snat_rules":[]}`},
		}}
		spec := &common.NetworkSpec{
			VPC:        common.VPC{ResourceID: "vpc-1"},
			NatGateway: &common.NatGatewaySpec{},
		}
		if err := newAdoptionTestManager(t, rt).ensureNatGateway(ctx, spec, "demo"); err != nil {
			t.Fatalf("ensureNatGateway: %v", err)
		}
		if spec.NatGateway.ResourceID != "nat-1" {
			t.Errorf("ResourceID = %q, want nat-1", spec.NatGateway.ResourceID)
		}
		assertNoWrites(t, rt)
	})

	t.Run("nat gateway conflict when not owned", func(t *testing.T) {
		rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
			{method: http.MethodGet, pathSub: "/nat_gateways/nat-1/tags", body: notOwnedTagsJSON},
			{method: http.MethodGet, pathSub: "/nat_gateways", body: `{"nat_gateways":[{"id":"nat-1","name":"demo-nat"}]}`},
		}}
		spec := &common.NetworkSpec{
			VPC:        common.VPC{ResourceID: "vpc-1"},
			NatGateway: &common.NatGatewaySpec{},
		}
		err := newAdoptionTestManager(t, rt).ensureNatGateway(ctx, spec, "demo")
		assertConflict(t, err, "NAT gateway", "demo-nat", "nat-1")
		if spec.NatGateway.ResourceID != "" {
			t.Errorf("ResourceID = %q, want empty (must not adopt)", spec.NatGateway.ResourceID)
		}
		assertNoWrites(t, rt)
	})
}

// TestDeleteNetworkAfterAdoptionConflictIsNoop rebuilds the exact reported
// failure: a managed network whose VPC name already exists (not provider-owned).
// Adoption is refused, so nothing is recorded in the spec and the later delete
// destroys nothing.
func TestDeleteNetworkAfterAdoptionConflictIsNoop(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRoundTripper{t: t, routes: []fakeRoute{
		{method: http.MethodGet, pathSub: "/vpcs/vpc-1/tags", body: notOwnedTagsJSON},
		{method: http.MethodGet, pathSub: "/vpcs", body: `{"vpcs":[{"id":"vpc-1","name":"demo-vpc"}]}`},
	}}
	m := newAdoptionTestManager(t, rt)
	spec := &common.NetworkSpec{VPC: common.VPC{Name: "demo-vpc"}}

	if err := m.ensureVpc(ctx, spec, "demo"); err == nil {
		t.Fatal("ensureVpc adopted a non-owned VPC; want conflict error")
	}
	if err := m.DeleteNetwork(ctx, spec, "demo"); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}
	assertNoWrites(t, rt)
}
