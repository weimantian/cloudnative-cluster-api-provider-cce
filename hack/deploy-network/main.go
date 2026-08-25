/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command deploy-network bootstraps the Huawei Cloud network resources used
// by the CCE deployment guide (docs/e2e-deployment-guide.md, stage 1):
//
//   - one VPC (10.0.0.0/16)
//   - two subnets: node subnet (10.0.1.0/24) and eni/container subnet
//     (10.0.2.0/24)
//   - one SSH keypair (capi-node-key)
//   - cheapest 2vCPU/4GiB ECS flavor in the region (CCE node minimum)
//
// It prints a CCE_DEPLOY_* env snippet consumed by the deploy guide (stage 1)
// and by deploy-bastion/deploy-mgmt-cluster when sourced. Credentials are read
// from CLOUD_SDK_AK / CLOUD_SDK_SK / CCE_DEPLOY_REGION (never hardcoded).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	ecsv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/model"
	ecsRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

const (
	vpcName     = "capi-vpc"
	subnetNode  = "capi-subnet-node"
	subnetENI   = "capi-subnet-eni"
	keypairName = "capi-node-key"
	vpcCIDR     = "10.0.0.0/16"
)

func main() {
	ctx := context.Background()
	ak := os.Getenv("CLOUD_SDK_AK")
	sk := os.Getenv("CLOUD_SDK_SK")
	regionID := os.Getenv("CCE_DEPLOY_REGION")
	if ak == "" || sk == "" {
		fatal("CLOUD_SDK_AK and CLOUD_SDK_SK must be set")
	}
	if regionID == "" {
		regionID = "cn-north-4"
	}

	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "build credentials")

	vpcRegionObj, err := vpcRegion.SafeValueOf(regionID)
	must(err, "resolve vpc region")
	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(vpcRegionObj).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build vpc client")
	vpcClient := vpcv2.NewVpcClient(vpcHC)

	ecsRegionObj, err := ecsRegion.SafeValueOf(regionID)
	must(err, "resolve ecs region")
	ecsHC, err := ecsv2.EcsClientBuilder().WithRegion(ecsRegionObj).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build ecs client")
	ecsClient := ecsv2.NewEcsClient(ecsHC)

	// 1. VPC (reuse if present).
	vpcID := findVPCByName(ctx, vpcClient, vpcName)
	if vpcID == "" {
		var resp *vpcmodel.CreateVpcResponse
		if err := retryThrottled("CreateVpc", 3, func() error {
			var e error
			resp, e = vpcClient.CreateVpc(&vpcmodel.CreateVpcRequest{Body: &vpcmodel.CreateVpcRequestBody{
				Vpc: &vpcmodel.CreateVpcOption{Name: stringPtr(vpcName), Cidr: stringPtr(vpcCIDR)},
			}})
			return e
		}); err != nil {
			must(err, "CreateVpc")
		}
		vpcID = mustID(resp.Vpc.Id, "vpc")
	}
	fmt.Printf("VPC: %s (%s)\n", vpcName, vpcID)

	// 2. Subnets (reuse by name).
	nodeSubnetID := findSubnetByName(ctx, vpcClient, vpcID, subnetNode)
	if nodeSubnetID == "" {
		nodeSubnetID = createSubnet(ctx, vpcClient, vpcID, subnetNode, "10.0.1.0/24")
	}
	eniSubnetID := findSubnetByName(ctx, vpcClient, vpcID, subnetENI)
	if eniSubnetID == "" {
		eniSubnetID = createSubnet(ctx, vpcClient, vpcID, subnetENI, "10.0.2.0/24")
	}
	nodeSubnetNeutron, eniSubnetNeutron := neutronSubnetIDs(ctx, vpcClient, vpcID)
	fmt.Printf("Node subnet: %s (id=%s neutron=%s)\n", subnetNode, nodeSubnetID, nodeSubnetNeutron)
	fmt.Printf("ENI  subnet: %s (id=%s neutron=%s)\n", subnetENI, eniSubnetID, eniSubnetNeutron)

	// 3. Keypair (nova API; reuse if present).
	keypairs, err := ecsClient.NovaListKeypairs(&model.NovaListKeypairsRequest{})
	if err == nil {
		for _, kp := range derefKeypairs(keypairs) {
			if kp.Keypair != nil && kp.Keypair.Name == keypairName {
				fmt.Printf("Keypair: %s (exists)\n", keypairName)
				goto keypairDone
			}
		}
	}
	{
		if err := retryThrottled("CreateKeypair", 3, func() error {
			_, e := ecsClient.NovaCreateKeypair(&model.NovaCreateKeypairRequest{Body: &model.NovaCreateKeypairRequestBody{
				Keypair: &model.NovaCreateKeypairOption{Name: keypairName},
			}})
			return e
		}); err != nil {
			must(err, "CreateKeypair")
		}
		fmt.Printf("Keypair: %s (created)\n", keypairName)
	}
keypairDone:

	// 4. Cheapest 2vCPU/4GiB flavor.
	flavor, err := cheapestFlavor(ctx, ecsClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: flavor lookup failed: %v\n", err)
	} else {
		fmt.Printf("Cheapest 2C4G flavor: %s (%s)\n", flavor.Name, flavor.Id)
	}

	fmt.Println("\n--- export for the deploy guide (stage 1) ---")
	fmt.Printf("export CCE_DEPLOY_REGION=%q\n", regionID)
	fmt.Printf("export CCE_DEPLOY_VPC=%q\n", vpcID)
	fmt.Printf("export CCE_DEPLOY_SUBNET=%q\n", nodeSubnetID)
	// eniNetwork.subnets[].subnetID requires the NEUTRON subnet id (official
	// CreateCluster doc; verified by the real CCE smoke test).
	if eniSubnetNeutron != "" {
		eniSubnetID = eniSubnetNeutron
	}
	fmt.Printf("export CCE_DEPLOY_ENI_SUBNET=%q  # neutron_subnet_id\n", eniSubnetID)
	fmt.Printf("export CCE_DEPLOY_KEYPAIR=%q\n", keypairName)
	if flavor != nil {
		fmt.Printf("export CCE_DEPLOY_FLAVOR=%q\n", flavor.Id)
	}
	fmt.Println("export CCE_DEPLOY_CLUSTER_FLAVOR='cce.s1.small'  # cheapest cluster (1 control node)")
}

func createSubnet(ctx context.Context, c *vpcv2.VpcClient, vpcID, name, cidr string) string {
	gw := cidr[:strings.LastIndex(cidr, ".")] + ".1"
	var resp *vpcmodel.CreateSubnetResponse
	if err := retryThrottled("CreateSubnet "+name, 3, func() error {
		var e error
		resp, e = c.CreateSubnet(&vpcmodel.CreateSubnetRequest{Body: &vpcmodel.CreateSubnetRequestBody{
			Subnet: &vpcmodel.CreateSubnetOption{Name: name, Cidr: cidr, VpcId: vpcID, GatewayIp: gw, PrimaryDns: stringPtr("100.125.1.250"), SecondaryDns: stringPtr("100.125.129.250")},
		}})
		return e
	}); err != nil {
		must(err, "CreateSubnet "+name)
	}
	return mustID(resp.Subnet.Id, "subnet")
}

func findVPCByName(ctx context.Context, c *vpcv2.VpcClient, name string) string {
	resp, err := c.ListVpcs(&vpcmodel.ListVpcsRequest{})
	if err != nil {
		return ""
	}
	for _, v := range derefVpcs(resp) {
		if v.Name == name {
			return v.Id
		}
	}
	return ""
}

func neutronSubnetIDs(ctx context.Context, c *vpcv2.VpcClient, vpcID string) (node, eni string) {
	resp, err := c.ListSubnets(&vpcmodel.ListSubnetsRequest{VpcId: &vpcID})
	if err != nil {
		return "", ""
	}
	for _, s := range derefSubnets(resp) {
		switch s.Name {
		case subnetNode:
			node = s.NeutronSubnetId
		case subnetENI:
			eni = s.NeutronSubnetId
		}
	}
	return node, eni
}

func findSubnetByName(ctx context.Context, c *vpcv2.VpcClient, vpcID, name string) string {
	resp, err := c.ListSubnets(&vpcmodel.ListSubnetsRequest{VpcId: &vpcID})
	if err != nil {
		return ""
	}
	for _, s := range derefSubnets(resp) {
		if s.Name == name {
			return s.Id
		}
	}
	return ""
}

func cheapestFlavor(ctx context.Context, c *ecsv2.EcsClient) (*model.Flavor, error) {
	resp, err := c.ListFlavors(&model.ListFlavorsRequest{})
	if err != nil {
		return nil, err
	}
	var candidates []model.Flavor
	for _, f := range derefFlavors(resp) {
		vcpus, _ := strconv.Atoi(f.Vcpus)
		if vcpus < 2 || f.Ram < 4096 {
			continue
		}
		// CCE Turbo (eni) requires a flavor with sub-ENI quota > 0
		// (verified: c6.large.2 -> "subeni quota is 0, Eni network is not
		// supported"; official error CCE.01400025).
		if f.OsExtraSpecs == nil || f.OsExtraSpecs.QuotasubNetworkInterfaceMaxNum == nil {
			continue
		}
		if subeni, _ := strconv.Atoi(*f.OsExtraSpecs.QuotasubNetworkInterfaceMaxNum); subeni <= 0 {
			continue
		}
		candidates = append(candidates, f)
	}
	sort.Slice(candidates, func(i, j int) bool {
		vi, _ := strconv.Atoi(candidates[i].Vcpus)
		vj, _ := strconv.Atoi(candidates[j].Vcpus)
		if vi != vj {
			return vi < vj
		}
		return candidates[i].Ram < candidates[j].Ram
	})
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no 2C4G Turbo-capable flavor found")
	}
	// Prefer common x86 general-purpose families (more likely purchasable).
	// c6sne.* is excluded: it reports sub-ENI quota > 0 but is abandon(ed) in
	// cn-north-4a (verified live: CreateNodePool fails with "flavor status is
	// abandon"), so prefer c7./c6ne. etc. which are active.
	for _, prefix := range []string{"c7.", "c7n.", "c6ne.", "c7t.", "s7.", "ac7.", "e7.", "t6.", "c6.", "s6."} {
		for i := range candidates {
			if strings.HasPrefix(candidates[i].Id, prefix) {
				return &candidates[i], nil
			}
		}
	}
	return &candidates[0], nil
}

func mustID(id, kind string) string {
	if id == "" {
		fatal("create " + kind + " returned no id")
	}
	return id
}

func stringPtr(s string) *string { return &s }

func derefVpcs(resp *vpcmodel.ListVpcsResponse) []vpcmodel.Vpc {
	if resp == nil || resp.Vpcs == nil {
		return nil
	}
	return *resp.Vpcs
}

func derefSubnets(resp *vpcmodel.ListSubnetsResponse) []vpcmodel.Subnet {
	if resp == nil || resp.Subnets == nil {
		return nil
	}
	return *resp.Subnets
}

func derefFlavors(resp *model.ListFlavorsResponse) []model.Flavor {
	if resp == nil || resp.Flavors == nil {
		return nil
	}
	return *resp.Flavors
}

func derefKeypairs(resp *model.NovaListKeypairsResponse) []model.NovaListKeypairsResult {
	if resp == nil || resp.Keypairs == nil {
		return nil
	}
	return *resp.Keypairs
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

// retryThrottled retries fn when the Huawei Cloud API reports 429
// (APIGW.0308 throttling). Each retry sleeps to let the per-minute write
// window drain before trying again, so repeated attempts do not keep
// refreshing the counter (verified: retries count towards the limit).
func retryThrottled(desc string, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err = fn()
		if !isThrottled(err) {
			return err
		}
		wait := time.Duration(60*(attempt+1)) * time.Second
		fmt.Printf("%s: throttled (429), retrying in %v (attempt %d/%d)\n", desc, wait, attempt+1, maxRetries)
		time.Sleep(wait)
	}
	return err
}

// isThrottled reports whether err is a Huawei Cloud 429 (APIGW.0308) error.
func isThrottled(err error) bool {
	if err == nil {
		return false
	}
	var se *sdkerr.ServiceResponseError
	if errors.As(err, &se) {
		return se.StatusCode == 429
	}
	return false
}
