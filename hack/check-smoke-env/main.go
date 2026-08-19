package main

import (
	"fmt"
	"os"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

func main() {
	cred, err := basic.NewCredentialsBuilder().WithAk(os.Getenv("AK")).WithSk(os.Getenv("SK")).SafeBuild()
	must(err)
	// VPCs
	vr, _ := vpcRegion.SafeValueOf("cn-north-4")
	hc, _ := vpcv2.VpcClientBuilder().WithRegion(vr).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	v := vpcv2.NewVpcClient(hc)
	vres, err := v.ListVpcs(&vpcmodel.ListVpcsRequest{})
	must(err)
	for _, vpc := range *vres.Vpcs {
		fmt.Printf("VPC %s %s cidr=%s\n", vpc.Id, vpc.Name, vpc.Cidr)
	}
	// Subnets
	sres, err := v.ListSubnets(&vpcmodel.ListSubnetsRequest{})
	must(err)
	for _, s := range *sres.Subnets {
		fmt.Printf("  SUBNET %s %s vpc=%s cidr=%s neutron=%s\n", s.Id, s.Name, s.VpcId, s.Cidr, s.NeutronSubnetId)
	}
	// CCE clusters (leftovers?)
	cr, _ := cceRegion.SafeValueOf("cn-north-4")
	chc, _ := ccev3.CceClientBuilder().WithRegion(cr).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	c := ccev3.NewCceClient(chc)
	cres, err := c.ListClusters(&model.ListClustersRequest{})
	must(err)
	n := 0
	if cres.Items != nil {
		for _, cl := range *cres.Items {
			if cl.Metadata != nil {
				fmt.Printf("CLUSTER %s %s phase=%v\n", *cl.Metadata.Uid, cl.Metadata.Name, *cl.Status.Phase)
				n++
			}
		}
	}
	fmt.Printf("total clusters: %d\n", n)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
