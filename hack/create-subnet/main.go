/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command create-subnet creates a VPC subnet. It exists because the workload
// ("pod") VPC used by the E2E test ships without a subnet, and CCE requires a
// node subnet before creating a workload cluster there.
//
// Env:
//
//	CCE_SMOKE_AK / CLOUD_SDK_AK
//	CCE_SMOKE_SK / CLOUD_SDK_SK
//	CCE_SMOKE_REGION   (default cn-north-4)
//
// Flags:
//
//	-vpc <id>       VPC to create the subnet in (required)
//	-cidr <cidr>    subnet CIDR (default 10.1.0.0/24)
//	-name <name>    subnet name (default pod-subnet)
//	-az <az>        availability zone (default cn-north-4a)
//	-gateway <ip>   gateway IP (default: first host address of -cidr)
//
// Usage:
//
//	go run ./hack/create-subnet -vpc 9c4c6207-a38a-4c5c-a814-43fd581a53d9
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

func main() {
	vpcID := flag.String("vpc", "", "VPC ID to create the subnet in")
	cidr := flag.String("cidr", "10.1.0.0/24", "subnet CIDR")
	name := flag.String("name", "pod-subnet", "subnet name")
	az := flag.String("az", "cn-north-4a", "availability zone")
	gateway := flag.String("gateway", "", "gateway IP (default: first host address of -cidr)")
	flag.Parse()

	if *vpcID == "" {
		fatal("-vpc is required")
	}

	ak := envOr("CCE_SMOKE_AK", "CLOUD_SDK_AK")
	sk := envOr("CCE_SMOKE_SK", "CLOUD_SDK_SK")
	region := envOr("CCE_SMOKE_REGION", "cn-north-4")
	if ak == "" || sk == "" {
		fatal("CCE_SMOKE_AK (or CLOUD_SDK_AK) and CCE_SMOKE_SK (or CLOUD_SDK_SK) must be set")
	}

	gw := *gateway
	if gw == "" {
		var err error
		gw, err = firstHost(*cidr)
		if err != nil {
			fatalf("invalid -cidr %q: %v", *cidr, err)
		}
	}

	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		fatalf("credentials: %v", err)
	}
	r, err := vpcregion.SafeValueOf(region)
	if err != nil {
		fatalf("region %q: %v", region, err)
	}
	hc, err := vpcv2.VpcClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		fatalf("vpc client: %v", err)
	}
	vc := vpcv2.NewVpcClient(hc)

	azPtr := *az
	req := &vpcmodel.CreateSubnetRequest{
		Body: &vpcmodel.CreateSubnetRequestBody{
			Subnet: &vpcmodel.CreateSubnetOption{
				Name:             *name,
				Cidr:             *cidr,
				VpcId:            *vpcID,
				GatewayIp:        gw,
				AvailabilityZone: &azPtr,
			},
		},
	}
	resp, err := vc.CreateSubnet(req)
	if err != nil {
		fatalf("CreateSubnet: %v", err)
	}
	if resp.Subnet == nil {
		fatal("CreateSubnet returned no subnet")
	}
	fmt.Printf("subnet created: name=%s\n", resp.Subnet.Name)
	fmt.Printf("  id=%s\n", resp.Subnet.Id)
	fmt.Printf("  neutron_subnet_id=%s\n", resp.Subnet.NeutronSubnetId)
	fmt.Printf("  cidr=%s gateway=%s vpc=%s\n", resp.Subnet.Cidr, resp.Subnet.GatewayIp, resp.Subnet.VpcId)
	fmt.Printf("\nCCE_E2E_SUBNET_ID=%s\n", resp.Subnet.Id)
}

// firstHost returns the first usable host address of a CIDR (network + 1).
func firstHost(cidr string) (string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	ip4 := ipnet.IP.To4()
	if ip4 == nil {
		return "", fmt.Errorf("only IPv4 is supported")
	}
	host := net.IPv4(ip4[0], ip4[1], ip4[2], ip4[3]+1)
	return host.String(), nil
}

func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "ERROR:", msg)
	os.Exit(1)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
