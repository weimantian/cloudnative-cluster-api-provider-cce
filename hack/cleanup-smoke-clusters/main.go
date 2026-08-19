package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
)

func main() {
	cred, err := basic.NewCredentialsBuilder().WithAk(os.Getenv("CLOUD_SDK_AK")).WithSk(os.Getenv("CLOUD_SDK_SK")).SafeBuild()
	must(err)
	r, err := cceRegion.SafeValueOf("cn-north-4")
	must(err)
	hc, err := ccev3.CceClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err)
	c := ccev3.NewCceClient(hc)
	resp, err := c.ListClusters(&model.ListClustersRequest{})
	must(err)
	n := 0
	if resp.Items != nil {
		for _, cl := range *resp.Items {
			if cl.Metadata == nil {
				continue
			}
			if !strings.HasPrefix(cl.Metadata.Name, "capi-") {
				continue
			}
			// delete node pools first
			pools, _ := c.ListNodePools(&model.ListNodePoolsRequest{ClusterId: *cl.Metadata.Uid})
			if pools.Items != nil {
				for _, p := range *pools.Items {
					if p.Metadata != nil {
						_, _ = c.DeleteNodePool(&model.DeleteNodePoolRequest{ClusterId: *cl.Metadata.Uid, NodepoolId: *p.Metadata.Uid})
					}
				}
			}
			ve := model.GetDeleteClusterRequestDeleteEvsEnum().BLOCK
			vi := model.GetDeleteClusterRequestDeleteEniEnum().BLOCK
			vn := model.GetDeleteClusterRequestDeleteNetEnum().BLOCK
			vo := model.GetDeleteClusterRequestOndemandNodePolicyEnum().DELETE
			vp := model.GetDeleteClusterRequestPeriodicNodePolicyEnum().RESET
			_, err := c.DeleteCluster(&model.DeleteClusterRequest{ClusterId: *cl.Metadata.Uid, DeleteEvs: &ve, DeleteEni: &vi, DeleteNet: &vn, OndemandNodePolicy: &vo, PeriodicNodePolicy: &vp})
			fmt.Printf("delete %s (uid=%s): %v\n", cl.Metadata.Name, *cl.Metadata.Uid, err)
			n++
		}
	}
	if n == 0 {
		fmt.Println("no capi-* clusters")
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
