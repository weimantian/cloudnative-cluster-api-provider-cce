/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command nat-egress restores public egress for a CCE management VPC by
// provisioning a small NAT gateway + EIP + SNAT rule, so cluster nodes can
// pull images from quay.io / registry.k8s.io (needed for clusterctl init).
//
// This is a one-off operations tool (not part of the provider binary). It:
//  1. create  — creates an EIP, a small NAT gateway in the node subnet, and a
//     SNAT rule mapping the node subnet to that EIP; polls until the gateway
//     is ACTIVE. Idempotent: if a gateway named <prefix>-nat already exists it
//     is reused instead of creating a duplicate.
//  2. list    — lists NAT gateways and their SNAT rules for the region.
//  3. delete  — deletes the SNAT rules, NAT gateway and EIP for a given
//     gateway ID (as printed by create/list).
//  4. delete-all — deletes every NAT gateway whose name matches <prefix>-nat,
//     plus their SNAT rules and EIPs.
//
// Usage:
//
//	CCE_DEPLOY_AK=... CCE_DEPLOY_SK=... CCE_DEPLOY_VPC=<vpc-id> \
//	  CCE_DEPLOY_SUBNET=<subnet-id> \
//	  go run ./hack/nat-egress -mode create
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
	natv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2"
	natmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/model"
	natRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/region"
)

const (
	natNamePrefix = "capi-egress"
	eipNamePrefix = "capi-egress-eip"
)

func main() {
	mode := flag.String("mode", "create", "create | list | delete | delete-all | delete-eip")
	gatewayID := flag.String("id", "", "NAT gateway ID (delete mode) or EIP ID (delete-eip mode)")
	flag.Parse()

	ak := envOr("CCE_DEPLOY_AK", "CLOUD_SDK_AK")
	sk := envOr("CCE_DEPLOY_SK", "CLOUD_SDK_SK")
	region := envOr("CCE_DEPLOY_REGION", "cn-north-4")
	if ak == "" || sk == "" {
		fatal("CCE_DEPLOY_AK (or CLOUD_SDK_AK) and CCE_DEPLOY_SK (or CLOUD_SDK_SK) must be set")
	}

	ctx := context.Background()
	natc := newNatClient(region, ak, sk)

	switch *mode {
	case "create":
		vpcID := envOr("CCE_DEPLOY_VPC")
		subnetID := envOr("CCE_DEPLOY_SUBNET")
		if vpcID == "" || subnetID == "" {
			fatal("create mode requires CCE_DEPLOY_VPC and CCE_DEPLOY_SUBNET")
		}
		doCreate(ctx, region, ak, sk, natc, vpcID, subnetID)
	case "list":
		doList(ctx, natc)
	case "delete":
		if *gatewayID == "" {
			fatal("delete mode requires -id <nat-gateway-id>")
		}
		doDelete(ctx, region, ak, sk, natc, *gatewayID)
	case "delete-eip":
		if *gatewayID == "" {
			fatal("delete-eip mode requires -id <eip-id>")
		}
		eipc := newEipClient(region, ak, sk)
		if err := deletePublicIP(ctx, eipc, *gatewayID); err != nil {
			fatalf("delete EIP %s: %v", *gatewayID, err)
		}
		fmt.Printf("deleted EIP id=%s\n", *gatewayID)
	case "delete-all":
		doDeleteAll(ctx, region, ak, sk, natc)
	default:
		fatal("unknown mode: " + *mode)
	}
}

// doCreate provisions (or reuses) a NAT gateway + EIP + SNAT rule and prints the IDs.
func doCreate(ctx context.Context, region, ak, sk string, natc *natv2.NatClient, vpcID, subnetID string) {
	natName := natNamePrefix + "-nat"

	// 1. Reuse an existing gateway if present (idempotency).
	if gw := findGatewayByName(ctx, natc, natName); gw != nil && gw.Id != nil {
		id := *gw.Id
		fmt.Printf("reusing existing NAT gateway id=%s name=%q status=%s\n", id, natName, statusOf(gw))
		ensureSnatRule(ctx, natc, id, subnetID)
		return
	}

	// 2. Create EIP, then NAT gateway, then poll ACTIVE.
	eipc := newEipClient(region, ak, sk)
	eipID, eipAddr, err := createPublicIP(ctx, eipc, eipNamePrefix)
	if err != nil {
		fatalf("create EIP: %v", err)
	}
	fmt.Printf("created EIP id=%s addr=%s\n", eipID, eipAddr)

	gwID, err := createNatGateway(ctx, natc, natName, vpcID, subnetID)
	if err != nil {
		fatalf("create NAT gateway: %v", err)
	}
	fmt.Printf("created NAT gateway id=%s — waiting for ACTIVE…\n", gwID)
	if err := waitNatGatewayActive(ctx, natc, gwID); err != nil {
		fatalf("wait NAT gateway ACTIVE: %v", err)
	}
	fmt.Printf("NAT gateway id=%s ACTIVE\n", gwID)

	// 3. SNAT rule mapping the node subnet to the EIP.
	ensureSnatRuleWithEip(ctx, natc, gwID, subnetID, eipID)

	fmt.Println("---- summary ----")
	fmt.Printf("NAT_GATEWAY_ID=%s\n", gwID)
	fmt.Printf("EIP_ID=%s\n", eipID)
	fmt.Printf("EIP_ADDRESS=%s\n", eipAddr)
	fmt.Printf("SUBNET_ID=%s\n", subnetID)
}

func doList(ctx context.Context, natc *natv2.NatClient) {
	resp, err := natc.ListNatGateways(&natmodel.ListNatGatewaysRequest{})
	if err != nil {
		fatalf("ListNatGateways: %v", err)
	}
	if resp.NatGateways == nil || len(*resp.NatGateways) == 0 {
		fmt.Println("(no NAT gateways)")
		return
	}
	for _, gw := range *resp.NatGateways {
		id, name, status := "-", "-", "-"
		if gw.Id != nil {
			id = *gw.Id
		}
		if gw.Name != nil {
			name = *gw.Name
		}
		if gw.Status != nil {
			status = gw.Status.Value()
		}
		fmt.Printf("gateway id=%s name=%q status=%s\n", id, name, status)
		for _, r := range listSnatRules(ctx, natc, id) {
			fmt.Printf("  snat-rule id=%s network_id=%s cidr=%s eip=%s status=%s\n",
				r.Id, r.NetworkId, r.Cidr, r.FloatingIpAddress, r.Status.Value())
		}
	}
}

func doDelete(ctx context.Context, region, ak, sk string, natc *natv2.NatClient, gatewayID string) {
	deleteGateway(ctx, region, ak, sk, natc, gatewayID)
}

func doDeleteAll(ctx context.Context, region, ak, sk string, natc *natv2.NatClient) {
	resp, err := natc.ListNatGateways(&natmodel.ListNatGatewaysRequest{})
	if err != nil {
		fatalf("ListNatGateways: %v", err)
	}
	if resp.NatGateways == nil || len(*resp.NatGateways) == 0 {
		fmt.Println("(no NAT gateways)")
		return
	}
	for _, gw := range *resp.NatGateways {
		if gw.Name == nil || !strings.HasPrefix(*gw.Name, natNamePrefix) {
			continue
		}
		if gw.Id == nil {
			continue
		}
		deleteGateway(ctx, region, ak, sk, natc, *gw.Id)
	}
}

// deleteGateway removes SNAT rules, then the NAT gateway, then the EIP(s).
func deleteGateway(ctx context.Context, region, ak, sk string, natc *natv2.NatClient, gatewayID string) {
	fmt.Printf("deleting NAT gateway id=%s…\n", gatewayID)

	eipIDs := map[string]bool{}
	for _, r := range listSnatRules(ctx, natc, gatewayID) {
		fmt.Printf("  deleting snat-rule id=%s\n", r.Id)
		if _, err := natc.DeleteNatGatewaySnatRule(&natmodel.DeleteNatGatewaySnatRuleRequest{
			NatGatewayId: gatewayID,
			SnatRuleId:   r.Id,
		}); err != nil {
			fmt.Printf("  WARN delete snat-rule %s: %v\n", r.Id, err)
			continue
		}
		if r.FloatingIpId != "" {
			eipIDs[r.FloatingIpId] = true
		}
	}
	// Wait for rules to drain before deleting the gateway.
	time.Sleep(5 * time.Second)

	if _, err := natc.DeleteNatGateway(&natmodel.DeleteNatGatewayRequest{NatGatewayId: gatewayID}); err != nil {
		fmt.Printf("WARN DeleteNatGateway: %v\n", err)
	} else {
		fmt.Printf("  NAT gateway id=%s delete request accepted\n", gatewayID)
	}

	// Release EIPs after the gateway is gone.
	if len(eipIDs) > 0 {
		time.Sleep(5 * time.Second)
		eipc := newEipClient(region, ak, sk)
		for id := range eipIDs {
			if err := deletePublicIP(ctx, eipc, id); err != nil {
				fmt.Printf("  WARN delete EIP %s: %v\n", id, err)
			} else {
				fmt.Printf("  deleted EIP id=%s\n", id)
			}
		}
	}
}

// ---- helpers ----

func newNatClient(region, ak, sk string) *natv2.NatClient {
	r, err := natRegion.SafeValueOf(region)
	must(err, "resolve NAT region")
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "build credentials")
	hc, err := natv2.NatClientBuilder().WithRegion(r).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build NAT client")
	return natv2.NewNatClient(hc)
}

func newEipClient(region, ak, sk string) *eipv2.EipClient {
	r, err := eipRegion.SafeValueOf(region)
	must(err, "resolve EIP region")
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "build credentials")
	hc, err := eipv2.EipClientBuilder().WithRegion(r).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build EIP client")
	return eipv2.NewEipClient(hc)
}

func createPublicIP(ctx context.Context, c *eipv2.EipClient, name string) (string, string, error) {
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

func deletePublicIP(ctx context.Context, c *eipv2.EipClient, id string) error {
	_, err := c.DeletePublicip(&eipmodel.DeletePublicipRequest{PublicipId: id})
	return err
}

func createNatGateway(ctx context.Context, c *natv2.NatClient, name, vpcID, subnetID string) (string, error) {
	spec := natmodel.GetCreateNatGatewayOptionSpecEnum().E_1
	resp, err := c.CreateNatGateway(&natmodel.CreateNatGatewayRequest{
		Body: &natmodel.CreateNatGatewayRequestBody{
			NatGateway: &natmodel.CreateNatGatewayOption{
				Name:              name,
				RouterId:          vpcID,
				InternalNetworkId: subnetID,
				Spec:              spec,
			},
		},
	})
	if err != nil {
		return "", err
	}
	if resp.NatGatewayId != nil {
		return *resp.NatGatewayId, nil
	}
	if resp.NatGateway != nil && resp.NatGateway.Id != nil {
		return *resp.NatGateway.Id, nil
	}
	return "", fmt.Errorf("no NAT gateway ID in response")
}

func findGatewayByName(ctx context.Context, c *natv2.NatClient, name string) *natmodel.NatGatewayResponseBody {
	resp, err := c.ListNatGateways(&natmodel.ListNatGatewaysRequest{Name: &name})
	if err != nil {
		return nil
	}
	if resp.NatGateways == nil || len(*resp.NatGateways) == 0 {
		return nil
	}
	gws := *resp.NatGateways
	return &gws[0]
}

func waitNatGatewayActive(ctx context.Context, c *natv2.NatClient, id string) error {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := c.ShowNatGateway(&natmodel.ShowNatGatewayRequest{NatGatewayId: id})
		if err != nil {
			return err
		}
		if resp.NatGateway != nil && resp.NatGateway.Status != nil &&
			resp.NatGateway.Status.Value() == "ACTIVE" {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("timed out waiting for ACTIVE")
}

func listSnatRules(ctx context.Context, c *natv2.NatClient, gatewayID string) []natmodel.NatGatewaySnatRuleResponseBody {
	resp, err := c.ListNatGatewaySnatRules(&natmodel.ListNatGatewaySnatRulesRequest{
		NatGatewayId: &[]string{gatewayID},
	})
	if err != nil {
		fmt.Printf("  WARN ListNatGatewaySnatRules: %v\n", err)
		return nil
	}
	if resp.SnatRules == nil {
		return nil
	}
	return *resp.SnatRules
}

func ensureSnatRule(ctx context.Context, c *natv2.NatClient, gatewayID, subnetID string) {
	// Reuse: reuse whatever EIP is already bound to the first rule.
	for _, r := range listSnatRules(ctx, c, gatewayID) {
		if r.NetworkId == subnetID {
			fmt.Printf("SNAT rule already exists id=%s eip=%s\n", r.Id, r.FloatingIpAddress)
			return
		}
	}
	fmt.Println("reused gateway has no SNAT rule for subnet — add one via -mode create with fresh gateway, or check console")
}

func ensureSnatRuleWithEip(ctx context.Context, c *natv2.NatClient, gatewayID, subnetID, eipID string) {
	for _, r := range listSnatRules(ctx, c, gatewayID) {
		if r.NetworkId == subnetID {
			fmt.Printf("SNAT rule already exists id=%s eip=%s\n", r.Id, r.FloatingIpAddress)
			return
		}
	}
	sourceType := int32(0)
	resp, err := c.CreateNatGatewaySnatRule(&natmodel.CreateNatGatewaySnatRuleRequest{
		Body: &natmodel.CreateNatGatewaySnatRuleRequestOption{
			SnatRule: &natmodel.CreateNatGatewaySnatRuleOption{
				NatGatewayId: gatewayID,
				NetworkId:    &subnetID,
				SourceType:   &sourceType,
				FloatingIpId: eipID,
			},
		},
	})
	if err != nil {
		fatalf("CreateNatGatewaySnatRule: %v", err)
	}
	id := ""
	if resp.SnatRule != nil {
		id = resp.SnatRule.Id
	}
	fmt.Printf("created SNAT rule id=%s subnet=%s eip=%s\n", id, subnetID, eipID)
}

func statusOf(gw *natmodel.NatGatewayResponseBody) string {
	if gw.Status != nil {
		return gw.Status.Value()
	}
	return "-"
}

func int32Ptr(v int32) *int32 { return &v }

func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func must(err error, ctx string) {
	if err != nil {
		fatalf("%s: %v", ctx, err)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "ERROR:", msg)
	os.Exit(1)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
