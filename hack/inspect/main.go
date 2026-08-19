package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
)

func main() {
	cred, _ := basic.NewCredentialsBuilder().WithAk(os.Getenv("AK")).WithSk(os.Getenv("SK")).SafeBuild()
	r, _ := cceRegion.SafeValueOf("cn-north-4")
	cc, err := ccev3.CceClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil { panic(err) }
	c := ccev3.NewCceClient(cc)
	clusterID := "09a17b0d-9bac-11f1-9003-0255ac10024c"
	pools, err := c.ListNodePools(&model.ListNodePoolsRequest{ClusterId: clusterID})
	if err != nil { fmt.Println("ListNodePools err:", err); return }
	b, _ := json.MarshalIndent(pools.Items, "", "  ")
	fmt.Println(string(b))
	nodes, err := c.ListNodes(&model.ListNodesRequest{ClusterId: clusterID})
	if err != nil { fmt.Println("ListNodes err:", err); return }
	b2, _ := json.MarshalIndent(nodes.Items, "", "  ")
	fmt.Println("NODES:", string(b2))
}
