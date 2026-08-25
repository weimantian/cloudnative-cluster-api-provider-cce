/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command deploy-mgmt-cluster creates a Huawei Cloud CCE management cluster
// (Standard, public API server) with a node pool, waits for it to become
// ready, and downloads its kubeconfig. It also supports listing and deleting
// clusters so the whole management-cluster lifecycle is one tool.
//
// The management cluster runs Cluster API (core) + this provider and manages
// workload clusters whose API server stays private (VPC-internal).
//
// Modes:
//
//	create      (default) create cluster + node pool, wait, fetch kubeconfig
//	-list       list all CCE clusters in the region and exit
//	-delete     delete the cluster given by -cluster (node pools first)
//	-delete-all delete every CCE cluster in the region (node pools first)
//
// Env (CCE_DEPLOY_* mirrors .env; CLOUD_SDK_AK/SK fall back for CI):
//
//	CCE_DEPLOY_AK / CLOUD_SDK_AK
//	CCE_DEPLOY_SK / CLOUD_SDK_SK
//	CCE_DEPLOY_REGION            (default cn-north-4)
//	CCE_DEPLOY_VPC               management VPC ID (create)
//	CCE_DEPLOY_SUBNET            management node subnet ID (create)
//	CCE_DEPLOY_KEYPAIR           SSH keypair for the node pool (create)
//	CCE_DEPLOY_AZ                availability zone (default cn-north-4a)
//	CCE_DEPLOY_MGMT_FLAVOR       node flavor (default c6.large.2)
//	CCE_DEPLOY_MGMT_NODES        node count (default 2)
//
// Usage:
//
//	go run ./hack/deploy-mgmt-cluster
//	go run ./hack/deploy-mgmt-cluster -list
//	go run ./hack/deploy-mgmt-cluster -delete -cluster <id>
//	go run ./hack/deploy-mgmt-cluster -delete-all
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
)

const (
	mgmtClusterWait  = 30 * time.Minute // cluster create -> Available
	mgmtPoolWait     = 30 * time.Minute // node pool -> active nodes
	mgmtPollInterval = 15 * time.Second
)

func main() {
	clusterID := flag.String("cluster", "", "existing CCE cluster ID (for -delete)")
	listMode := flag.Bool("list", false, "list all CCE clusters and exit")
	deleteMode := flag.Bool("delete", false, "delete the cluster given by -cluster")
	deleteAllMode := flag.Bool("delete-all", false, "delete every CCE cluster in the region")
	poolMode := flag.Bool("pool", false, "create node pool + fetch kubeconfig on existing -cluster")
	kubeconfigPath := flag.String("kubeconfig", "capi-mgmt.kubeconfig", "kubeconfig output path (create mode)")
	flag.Parse()

	ak := envOr("CCE_DEPLOY_AK", "CLOUD_SDK_AK")
	sk := envOr("CCE_DEPLOY_SK", "CLOUD_SDK_SK")
	region := envDefault("CCE_DEPLOY_REGION", "cn-north-4")
	if ak == "" || sk == "" {
		fatal("CCE_DEPLOY_AK (or CLOUD_SDK_AK) and CCE_DEPLOY_SK (or CLOUD_SDK_SK) must be set")
	}

	ctx := context.Background()
	svc, err := cce.NewClient(region, &credentials.Credentials{AccessKey: ak, SecretKey: sk})
	if err != nil {
		fatalf("NewClient: %v", err)
	}

	switch {
	case *listMode:
		listClusters(ctx, svc)
	case *deleteMode:
		if *clusterID == "" {
			fatal("-delete requires -cluster <id>")
		}
		deleteCluster(ctx, svc, *clusterID)
	case *deleteAllMode:
		deleteAllClusters(ctx, svc)
	case *poolMode:
		if *clusterID == "" {
			fatal("-pool requires -cluster <id>")
		}
		createPool(ctx, svc, *clusterID, *kubeconfigPath)
	default:
		createMgmtCluster(ctx, svc, *kubeconfigPath)
	}
}

func createMgmtCluster(ctx context.Context, svc cce.Service, kubeconfigPath string) {
	vpcID := envOr("CCE_DEPLOY_VPC")
	subnetID := envOr("CCE_DEPLOY_SUBNET")
	keypair := envOr("CCE_DEPLOY_KEYPAIR")
	if vpcID == "" || subnetID == "" || keypair == "" {
		fatal("CCE_DEPLOY_VPC, CCE_DEPLOY_SUBNET and CCE_DEPLOY_KEYPAIR are required to create")
	}

	name := "capi-mgmt-" + fmt.Sprintf("%d", time.Now().Unix()%100000)
	fmt.Printf("creating management cluster %q (region %s, vpc %s, subnet %s)…\n",
		name, envDefault("CCE_DEPLOY_REGION", "cn-north-4"), vpcID, subnetID)

	id, err := svc.CreateCluster(ctx, cce.CreateClusterInput{
		Name:                 name,
		Category:             "CCE", // Standard
		Flavor:               "cce.s1.small",
		Version:              envOr("CCE_DEPLOY_K8S_VERSION", ""),
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: "10.244.0.0/16",
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		PublicAccess:         true, // management API server reachable from the laptop
		BillingMode:          0,
	})
	if err != nil {
		fatalf("CreateCluster: %v", err)
	}
	fmt.Printf("cluster created: %s\n", id)

	info, err := waitForPhase(ctx, svc, id, "Available", mgmtClusterWait, mgmtPollInterval)
	if err != nil {
		fatalf("cluster did not become Available: %v", err)
	}
	fmt.Printf("cluster Available: version=%s\n", info.Version)
	for _, ep := range info.Endpoints {
		fmt.Printf("  endpoint type=%s url=%s\n", ep.Type, ep.URL)
	}

	createPool(ctx, svc, id, kubeconfigPath)
}

// createPool creates the management node pool on an existing cluster, waits
// for its nodes to become active, and downloads the kubeconfig. Shared by the
// default create mode and the standalone -pool mode.
func createPool(ctx context.Context, svc cce.Service, clusterID, kubeconfigPath string) {
	keypair := envOr("CCE_DEPLOY_KEYPAIR")
	az := envDefault("CCE_DEPLOY_AZ", "cn-north-4a")
	flavor := envDefault("CCE_DEPLOY_MGMT_FLAVOR", "c6.large.2")
	nodeCount := int32Env("CCE_DEPLOY_MGMT_NODES", 2)
	if keypair == "" {
		fatal("CCE_DEPLOY_KEYPAIR is required to create a node pool")
	}

	poolID, err := svc.CreateNodePool(ctx, cce.CreateNodePoolInput{
		ClusterID: clusterID,
		Name:      "mgmt-pool-0",
		Flavor:    flavor,
		// OS is required (verified live: "OS:should not be empty"); valid
		// value for current versions from official docs.
		OS:             "Huawei Cloud EulerOS 2.0",
		RootVolumeSize: 40,
		RootVolumeType: "GPSSD",
		// Non-local-disk flavors (c6.large.2) require a data volume.
		DataVolumes:      []cce.NodeVolumeInput{{Size: 100, Type: "GPSSD"}},
		SSHKey:           keypair,
		AvailabilityZone: az,
		InitialNodeCount: nodeCount,
		BillingMode:      0,
	})
	if err != nil {
		fatalf("CreateNodePool: %v", err)
	}
	fmt.Printf("node pool created: %s (flavor %s, nodes %d)\n", poolID, flavor, nodeCount)

	if err := waitForActiveNodeCount(ctx, svc, clusterID, poolID, nodeCount, mgmtPoolWait, mgmtPollInterval); err != nil {
		fatalf("node pool did not reach %d active nodes: %v", nodeCount, err)
	}
	fmt.Printf("node pool has %d active node(s)\n", nodeCount)

	kube, err := svc.GetClusterKubeconfig(ctx, clusterID, 30)
	if err != nil {
		fatalf("GetClusterKubeconfig: %v", err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte(kube), 0o600); err != nil {
		fatalf("write kubeconfig: %v", err)
	}
	fmt.Printf("kubeconfig written to %s (%d bytes)\n", kubeconfigPath, len(kube))
	fmt.Printf("\nMGMT_CLUSTER_ID=%s\nMGMT_POOL_ID=%s\n", clusterID, poolID)
	fmt.Println("done. If the public endpoint is not yet reachable, run hack/bind-eip -cluster", clusterID)
}

func listClusters(ctx context.Context, svc cce.Service) {
	refs, err := svc.ListClusters(ctx)
	if err != nil {
		fatalf("ListClusters: %v", err)
	}
	if len(refs) == 0 {
		fmt.Println("(no clusters)")
		return
	}
	for _, r := range refs {
		fmt.Printf("%s\t%s\n", r.ClusterID, r.Name)
	}
}

func deleteCluster(ctx context.Context, svc cce.Service, id string) {
	pools, err := svc.ListNodePools(ctx, id)
	if err != nil {
		fatalf("ListNodePools(%s): %v", id, err)
	}
	for _, p := range pools {
		fmt.Printf("deleting node pool %s…\n", p.NodePoolID)
		if err := svc.DeleteNodePool(ctx, id, p.NodePoolID); err != nil {
			fatalf("DeleteNodePool(%s): %v", p.NodePoolID, err)
		}
	}
	if len(pools) > 0 {
		waitForNodePoolGone(ctx, svc, id, mgmtPoolWait, mgmtPollInterval)
	}
	fmt.Printf("deleting cluster %s…\n", id)
	if err := svc.DeleteCluster(ctx, cce.DeleteClusterInput{
		ClusterID:          id,
		DeleteEVS:          true,
		DeleteENI:          true,
		DeleteELB:          true,
		OnDemandNodePolicy: "delete",
		PeriodicNodePolicy: "reset",
	}); err != nil {
		fatalf("DeleteCluster: %v", err)
	}
	waitForClusterGone(ctx, svc, id, mgmtClusterWait, mgmtPollInterval)
	fmt.Printf("cluster %s deleted\n", id)
}

func deleteAllClusters(ctx context.Context, svc cce.Service) {
	refs, err := svc.ListClusters(ctx)
	if err != nil {
		fatalf("ListClusters: %v", err)
	}
	if len(refs) == 0 {
		fmt.Println("no clusters to delete")
		return
	}
	for _, r := range refs {
		fmt.Printf("deleting cluster %s (%s)…\n", r.Name, r.ClusterID)
		deleteCluster(ctx, svc, r.ClusterID)
	}
}

// --- poll helpers (mirror internal/services/cce/smoke_test.go) ---

func waitForPhase(ctx context.Context, svc cce.Service, clusterID, want string, timeout, interval time.Duration) (*cce.ClusterInfo, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := svc.ShowCluster(ctx, clusterID)
		if err != nil {
			if isTemporary(err) {
				time.Sleep(interval)
				continue
			}
			return nil, err
		}
		fmt.Printf("  phase=%s (want %s)\n", info.Phase, want)
		if info.Phase == want {
			return info, nil
		}
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("cluster %s not in phase %q within %v", clusterID, want, timeout)
}

func waitForActiveNodeCount(ctx context.Context, svc cce.Service, clusterID, nodePoolID string, want int32, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pools, err := svc.ListNodePools(ctx, clusterID)
		if err == nil {
			for _, p := range pools {
				if p.NodePoolID == nodePoolID {
					fmt.Printf("  activeNodes=%d (want %d)\n", p.ActiveNodeCount, want)
					if p.ActiveNodeCount >= want {
						return nil
					}
				}
			}
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("node pool %s did not reach %d active nodes within %v", nodePoolID, want, timeout)
}

func waitForNodePoolGone(ctx context.Context, svc cce.Service, clusterID string, timeout, interval time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pools, err := svc.ListNodePools(ctx, clusterID)
		if err != nil || len(pools) == 0 {
			return
		}
		time.Sleep(interval)
	}
}

func waitForClusterGone(ctx context.Context, svc cce.Service, clusterID string, timeout, interval time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := svc.ShowCluster(ctx, clusterID); err != nil {
			return
		}
		time.Sleep(interval)
	}
}

func isTemporary(err error) bool {
	s := err.Error()
	return strings.Contains(s, "throttl") || strings.Contains(s, "lock") || strings.Contains(s, "concurrency")
}

// --- helpers ---

func envOr(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// envDefault returns the value of key, or def when the key is unset or empty.
func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func int32Env(key string, def int32) int32 {
	if v := os.Getenv(key); v != "" {
		var n int32
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "ERROR:", msg)
	os.Exit(1)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
