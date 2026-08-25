package main

// Command cleanup-hw tears down the real Huawei Cloud resources created by the
// smoke test: the CCE cluster (plus its node pool), the bound public EIP, and
// the VPC/subnet that hosted it.
//
// Huawei resource deletion is asynchronous and cascading: the node pool must
// finish deleting before the cluster can be deleted, the cluster must be gone
// before the EIP is unbound, the routes must be gone before the subnet can be
// deleted, and the subnet must be gone before the VPC can be deleted.
//
// This tool therefore drives deletion one resource at a time and POLLS until
// each resource is actually gone (or a timeout elapses) before moving on.

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	ccemodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

const (
	pollInterval = 10 * time.Second
	pollTimeout  = 20 * time.Minute
)

func main() {
	clusterID := flag.String("cluster", "", "CCE cluster ID to delete")
	nodepoolID := flag.String("nodepool", "", "node pool ID to delete")
	eipID := flag.String("eip", "", "public EIP ID to release")
	subnetID := flag.String("subnet", "", "subnet ID to delete")
	vpcID := flag.String("vpc", "", "VPC ID to delete")
	region := flag.String("region", "cn-north-4", "Huawei Cloud region")
	flag.Parse()

	if *clusterID == "" {
		fatal("usage: go run ./hack/cleanup-hw -cluster <id> [-nodepool <id>] [-eip <id>] [-subnet <id> -vpc <id>]")
	}

	ak := envOr("CLOUD_SDK_AK", "CCE_DEPLOY_AK")
	sk := envOr("CLOUD_SDK_SK", "CCE_DEPLOY_SK")
	if ak == "" || sk == "" {
		fatal("CLOUD_SDK_AK and CLOUD_SDK_SK must be set")
	}

	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "build credentials")

	cc := newCCEClient(*region, cred)
	ec := newEipClient(*region, cred)
	vc := newVpcClient(*region, cred)

	// 1. Node pool -> wait gone.
	if *nodepoolID != "" {
		if _, err := cc.DeleteNodePool(&ccemodel.DeleteNodePoolRequest{ClusterId: *clusterID, NodepoolId: *nodepoolID}); err != nil {
			fmt.Printf("DeleteNodePool: %v (continuing to poll)\n", err)
		}
		if err := pollNodePoolGone(cc, *clusterID, *nodepoolID); err != nil {
			fmt.Printf("node pool still present after timeout: %v\n", err)
		} else {
			fmt.Println("node pool gone")
		}
	}

	// 2. Cluster -> wait gone.
	{
		ve := ccemodel.GetDeleteClusterRequestDeleteEvsEnum().BLOCK
		vi := ccemodel.GetDeleteClusterRequestDeleteEniEnum().BLOCK
		vn := ccemodel.GetDeleteClusterRequestDeleteNetEnum().BLOCK
		vo := ccemodel.GetDeleteClusterRequestOndemandNodePolicyEnum().DELETE
		vp := ccemodel.GetDeleteClusterRequestPeriodicNodePolicyEnum().RESET
		if _, err := cc.DeleteCluster(&ccemodel.DeleteClusterRequest{
			ClusterId: *clusterID, DeleteEvs: &ve, DeleteEni: &vi, DeleteNet: &vn,
			OndemandNodePolicy: &vo, PeriodicNodePolicy: &vp,
		}); err != nil {
			fmt.Printf("DeleteCluster: %v (continuing to poll)\n", err)
		}
		if err := pollClusterGone(cc, *clusterID); err != nil {
			fmt.Printf("cluster still present after timeout: %v\n", err)
		} else {
			fmt.Println("cluster gone")
		}
	}

	// 3. EIP -> wait gone.
	if *eipID != "" {
		if _, err := ec.DeletePublicip(&eipmodel.DeletePublicipRequest{PublicipId: *eipID}); err != nil {
			fmt.Printf("DeletePublicip: %v (continuing to poll)\n", err)
		}
		if err := pollEipGone(ec, *eipID); err != nil {
			fmt.Printf("EIP still present after timeout: %v\n", err)
		} else {
			fmt.Println("EIP gone")
		}
	}

	// 4. Subnet then VPC -> wait each gone.
	if *subnetID != "" && *vpcID != "" {
		if _, err := vc.DeleteSubnet(&vpcmodel.DeleteSubnetRequest{VpcId: *vpcID, SubnetId: *subnetID}); err != nil {
			fmt.Printf("DeleteSubnet: %v (continuing to poll)\n", err)
		}
		if err := pollSubnetGone(vc, *subnetID); err != nil {
			fmt.Printf("subnet still present after timeout: %v\n", err)
		} else {
			fmt.Println("subnet gone")
		}
		if _, err := vc.DeleteVpc(&vpcmodel.DeleteVpcRequest{VpcId: *vpcID}); err != nil {
			fmt.Printf("DeleteVpc: %v (continuing to poll)\n", err)
		}
		if err := pollVpcGone(vc, *vpcID); err != nil {
			fmt.Printf("VPC still present after timeout: %v\n", err)
		} else {
			fmt.Println("VPC gone")
		}
	}

	fmt.Println("cleanup complete")
}

func pollNodePoolGone(c *ccev3.CceClient, clusterID, nodepoolID string) error {
	return pollUntil(pollInterval, pollTimeout, func() (bool, error) {
		resp, err := c.ListNodePools(&ccemodel.ListNodePoolsRequest{ClusterId: clusterID})
		if err != nil {
			return false, nil // cluster may already be gone; keep polling
		}
		if resp.Items != nil {
			for _, p := range *resp.Items {
				if p.Metadata != nil && p.Metadata.Uid != nil && *p.Metadata.Uid == nodepoolID {
					return false, nil
				}
			}
		}
		return true, nil
	})
}

func pollClusterGone(c *ccev3.CceClient, clusterID string) error {
	return pollUntil(pollInterval, pollTimeout, func() (bool, error) {
		resp, err := c.ListClusters(&ccemodel.ListClustersRequest{})
		if err != nil {
			return false, nil
		}
		if resp.Items != nil {
			for _, cl := range *resp.Items {
				if cl.Metadata != nil && cl.Metadata.Uid != nil && *cl.Metadata.Uid == clusterID {
					return false, nil
				}
			}
		}
		return true, nil
	})
}

func pollEipGone(c *eipv2.EipClient, eipID string) error {
	return pollUntil(pollInterval, pollTimeout, func() (bool, error) {
		_, err := c.ShowPublicip(&eipmodel.ShowPublicipRequest{PublicipId: eipID})
		if err != nil {
			return true, nil // Show returns error once the EIP is gone
		}
		return false, nil
	})
}

func pollSubnetGone(c *vpcv2.VpcClient, subnetID string) error {
	return pollUntil(pollInterval, pollTimeout, func() (bool, error) {
		_, err := c.ShowSubnet(&vpcmodel.ShowSubnetRequest{SubnetId: subnetID})
		if err != nil {
			return true, nil
		}
		return false, nil
	})
}

func pollVpcGone(c *vpcv2.VpcClient, vpcID string) error {
	return pollUntil(pollInterval, pollTimeout, func() (bool, error) {
		_, err := c.ShowVpc(&vpcmodel.ShowVpcRequest{VpcId: vpcID})
		if err != nil {
			return true, nil
		}
		return false, nil
	})
}

func pollUntil(interval, timeout time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		done, _ := check()
		if done {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

func newCCEClient(region string, cred auth.ICredential) *ccev3.CceClient {
	r, err := cceregion.SafeValueOf(region)
	must(err, "resolve CCE region")
	hc, err := ccev3.CceClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build CCE client")
	return ccev3.NewCceClient(hc)
}

func newEipClient(region string, cred auth.ICredential) *eipv2.EipClient {
	r, err := eipregion.SafeValueOf(region)
	must(err, "resolve EIP region")
	hc, err := eipv2.EipClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build EIP client")
	return eipv2.NewEipClient(hc)
}

func newVpcClient(region string, cred auth.ICredential) *vpcv2.VpcClient {
	r, err := vpcregion.SafeValueOf(region)
	must(err, "resolve VPC region")
	hc, err := vpcv2.VpcClientBuilder().WithRegion(r).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build VPC client")
	return vpcv2.NewVpcClient(hc)
}

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
