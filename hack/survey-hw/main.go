package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	ccemodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
	eipv3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v3"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v3/model"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v3/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
	ecsv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2"
	ecsmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/model"
	ecsregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/region"
)

func j(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func main() {
	ak := os.Getenv("CLOUD_SDK_AK")
	sk := os.Getenv("CLOUD_SDK_SK")
	if ak == "" {
		ak = "HPUANXUOD69NHMA22B1O"
	}
	if sk == "" {
		sk = "doPPU6gJmSKoOi047zs8dd8Cn0MYEuIQVQeQkS3z"
	}
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		fmt.Println("cred err:", err)
		return
	}

	fmt.Println("===== CCE CLUSTERS =====")
	r, _ := cceregion.SafeValueOf("cn-north-4")
	hc, _ := ccev3.CceClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	cc := ccev3.NewCceClient(hc)
	resp, err := cc.ListClusters(&ccemodel.ListClustersRequest{})
	if err != nil {
		fmt.Println("list clusters err:", err)
	} else {
		fmt.Println(j(resp.Items))
	}

	fmt.Println("===== EIPs =====")
	er, _ := eipregion.SafeValueOf("cn-north-4")
	ehc, _ := eipv3.EipClientBuilder().WithRegion(er).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	ec := eipv3.NewEipClient(ehc)
	eresp, err := ec.ListPublicips(&eipmodel.ListPublicipsRequest{})
	if err != nil {
		fmt.Println("list eip err:", err)
	} else {
		fmt.Println(j(eresp.Publicips))
	}

	fmt.Println("===== VPCs =====")
	vr, _ := vpcregion.SafeValueOf("cn-north-4")
	vhc, _ := vpcv2.VpcClientBuilder().WithRegion(vr).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	vc := vpcv2.NewVpcClient(vhc)
	vresp, err := vc.ListVpcs(&vpcmodel.ListVpcsRequest{})
	if err != nil {
		fmt.Println("list vpc err:", err)
	} else {
		fmt.Println(j(vresp.Vpcs))
	}

	fmt.Println("===== SUBNETS =====")
	sresp, err := vc.ListSubnets(&vpcmodel.ListSubnetsRequest{})
	if err != nil {
		fmt.Println("list subnets err:", err)
	} else {
		fmt.Println(j(sresp.Subnets))
	}

	fmt.Println("===== KEYPAIRS =====")
	kr, _ := ecsregion.SafeValueOf("cn-north-4")
	khc, _ := ecsv2.EcsClientBuilder().WithRegion(kr).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	kc := ecsv2.NewEcsClient(khc)
	kresp, kerr := kc.NovaListKeypairs(&ecsmodel.NovaListKeypairsRequest{})
	if kerr != nil {
		fmt.Println("list keypairs err:", kerr)
	} else if kresp.Keypairs != nil {
		for _, kp := range *kresp.Keypairs {
			if kp.Keypair != nil {
				fmt.Println("  KEYPAIR", kp.Keypair.Name)
			}
		}
	}
}
