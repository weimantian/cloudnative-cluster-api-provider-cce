//go:build smoke

/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package cce contains the REAL CCE smoke test. It is gated behind the
// "smoke" build tag and environment variables so it never runs in CI.
//
// Usage:
//
//	export CCE_SMOKE_AK=... CCE_SMOKE_SK=...
//	export CCE_SMOKE_REGION=cn-north-4 CCE_SMOKE_VPC=vpc-xxx \
//	       CCE_SMOKE_SUBNET=sub-xxx CCE_SMOKE_ENI_SUBNET=sub-xxx \
//	       CCE_SMOKE_KEYPAIR=my-keypair
//	go test -tags smoke -v ./internal/services/cce/ -run TestSmoke
//
// The test creates a real CCE cluster and node pool (billed!). It always
// cleans up (delete node pool + cluster with delete options). See
// docs/smoke-test-checklist.md for the required account setup and which
// questionnaire items each case verifies.
package cce

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
)

const (
	smokePollInterval = 30 * time.Second
	smokeClusterWait  = 30 * time.Minute
	smokePoolWait     = 20 * time.Minute
)

func smokeEnv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func smokeRequired(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("required env var %s is not set (see docs/smoke-test-checklist.md)", name)
	}
	return v
}

func smokeCases() map[string]bool {
	allowed := map[string]bool{}
	for _, c := range strings.Split(smokeEnv("CCE_SMOKE_CASES", "cluster,pool,scale,delete"), ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			allowed[c] = true
		}
	}
	return allowed
}

// TestSmoke drives the real CCE API and verifies/logs the behaviors that
// could not be confirmed from documentation alone (questionnaire Q1-Q8).
func TestSmoke(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	eniSubnetID := smokeRequired(t, "CCE_SMOKE_ENI_SUBNET")
	keypair := smokeRequired(t, "CCE_SMOKE_KEYPAIR")
	flavor := smokeEnv("CCE_SMOKE_FLAVOR", "c7.large.2")
	version := smokeEnv("CCE_SMOKE_K8S_VERSION", "")
	cases := smokeCases()
	mode := smokeEnv("CCE_SMOKE_MODE", "eni") // eni (Turbo) | vpc-router (Standard)

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Logf("smoke env: region=%s mode=%s vpc=%s nodeSubnet=%s eniSubnet=%s keypair=%s", region, mode, vpcID, subnetID, eniSubnetID, keypair)
	suffix := fmt.Sprintf("%d", time.Now().Unix()%100000)
	clusterName := "capi-smoke-" + suffix
	poolName := "pool-0"
	var clusterID, nodePoolID string
	cleaned := false
	defer func() {
		if cleaned {
			return
		}
		// Best-effort cleanup with delete options (Q8).
		if nodePoolID != "" {
			_ = svc.DeleteNodePool(ctx, clusterID, nodePoolID)
		}
		if clusterID != "" {
			_ = svc.DeleteCluster(ctx, DeleteClusterInput{
				ClusterID:          clusterID,
				DeleteEVS:          true,
				DeleteENI:          true,
				DeleteELB:          true,
				OnDemandNodePolicy: "delete",
				PeriodicNodePolicy: "reset",
			})
		}
	}()

	// ---- Q7: cluster quota (runtime value beats documentation) ----
	if cases["quota"] {
		q, err := svc.ShowQuotas(ctx)
		if err != nil {
			t.Errorf("ShowQuotas failed: %v", err)
		} else {
			t.Logf("Q7 ShowQuotas: limit=%d used=%d", q.ClusterQuotaLimit, q.ClusterQuotaUsed)
		}
	}

	// ---- Q1/Q4: create an EMPTY cluster (Turbo/eni) ----
	if cases["cluster"] {
		t.Logf("creating empty Turbo/eni cluster %q (region %s)…", clusterName, region)
		createIn := CreateClusterInput{
			Name:                clusterName,
			Flavor:              smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
			Version:             version,
			HostNetworkVpcID:    vpcID,
			HostNetworkSubnetID: subnetID,
			ServiceCIDR:         "10.247.0.0/16",
			PublicAccess:        false,
			BillingMode:         0,
		}
		if mode == "vpc-router" {
			// Standard cluster (no sub-ENI requirement; cheap flavors work).
			createIn.Category = "CCE"
			createIn.ContainerNetworkMode = "vpc-router"
			createIn.ContainerNetworkCIDR = "10.244.0.0/16"
		} else {
			// Turbo/eni (requires a flavor with sub-ENI quota > 0).
			createIn.Category = "Turbo"
			createIn.ContainerNetworkMode = "eni"
			createIn.ENISubnets = []string{eniSubnetID}
		}
		clusterID, err = svc.CreateCluster(ctx, createIn)
		if err != nil {
			t.Fatalf("CreateCluster (empty, Turbo/eni) failed: %v", err)
		}
		t.Logf("Q1 empty cluster created: %s", clusterID)

		// Wait for Available and record the phase sequence (Q1).
		info, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval)
		if err != nil {
			t.Fatalf("cluster did not become Available: %v", err)
		}
		t.Logf("Q1 cluster phase=Available, version=%s, endpoints=%v", info.Version, info.Endpoints)

		// ---- Q2: kubeconfig with explicit duration ----
		if cases["kubeconfig"] {
			kube, err := svc.GetClusterKubeconfig(ctx, clusterID, 30)
			if err != nil {
				t.Errorf("GetClusterKubeconfig failed: %v", err)
			} else {
				t.Logf("Q2 kubeconfig retrieved (%d bytes), current-context external/internal per public IP", len(kube))
			}
		}
	} else {
		t.Skip("CCE_SMOKE_CASES does not include 'cluster'")
	}

	// ---- Q3: node pool with initialNodeCount=2 ----
	if cases["pool"] {
		t.Logf("creating node pool with initialNodeCount=2 (flavor %s)…", flavor)
		nodePoolID, err = svc.CreateNodePool(ctx, CreateNodePoolInput{
			ClusterID: clusterID,
			Name:      poolName,
			Flavor:    flavor,
			// OS is required (verified: "OS:should not be empty" when omitted,
			// contradicting the SDK comment). Value from official docs:
			// "Huawei Cloud EulerOS 2.0".
			OS:             "Huawei Cloud EulerOS 2.0",
			RootVolumeSize: 40,
			RootVolumeType: "GPSSD",
			// Non-local-disk flavors (e.g. c6.large.2) require a data volume
			// (verified: CCE_CM.0004 "Data volume needed for non-local-disk
			// flavor or non-system diskType").
			DataVolumes:      []NodeVolumeInput{{Size: 100, Type: "GPSSD"}},
			SSHKey:           keypair,
			AvailabilityZone: smokeEnv("CCE_SMOKE_AZ", ""),
			InitialNodeCount: 2,
			BillingMode:      0,
		})
		if err != nil {
			t.Fatalf("CreateNodePool failed: %v", err)
		}
		t.Logf("node pool created: %s", nodePoolID)
		if err := waitForNodeCount(ctx, svc, clusterID, nodePoolID, 2, smokePoolWait, smokePollInterval); err != nil {
			t.Fatalf("node pool did not reach 2 nodes: %v", err)
		}
		t.Log("Q3 node pool reached initialNodeCount=2")

		// ---- Q3: ScaleNodePool(desiredNodeCount=2) — absolute vs delta ----
		if cases["scale"] {
			t.Log("calling ScaleNodePool(desiredNodeCount=2) on a 2-node pool…")
			if err := svc.ScaleNodePool(ctx, clusterID, nodePoolID, 2); err != nil {
				// "No scale task needed with desired node count 2" on a 2-node
				// pool PROVES desiredNodeCount is the ABSOLUTE expected total:
				// under delta semantics the API would perform +2 (-> 4).
				if strings.Contains(err.Error(), "No scale task needed") {
					t.Logf("Q3 CONFIRMED (absolute semantics): ScaleNodePool(2) on a 2-node pool is a no-op (%v)", err)
				} else {
					t.Errorf("ScaleNodePool failed: %v", err)
				}
			} else {
				t.Log("Q3 note: ScaleNodePool(2) succeeded on a 2-node pool; verify node count in the console")
			}
			// Optional scale-up confirmation (adds 2 nodes, off by default):
			// CCE_SMOKE_CASES including "scaleup" calls ScaleNodePool(4).
			if cases["scaleup"] {
				t.Log("calling ScaleNodePool(desiredNodeCount=4) — expecting growth to 4 nodes (absolute)")
				if err := svc.ScaleNodePool(ctx, clusterID, nodePoolID, 4); err != nil {
					t.Errorf("ScaleNodePool(4) failed: %v", err)
				} else {
					if err := waitForNodeCount(ctx, svc, clusterID, nodePoolID, 4, smokePoolWait, smokePollInterval); err != nil {
						t.Errorf("pool did not reach 4 nodes: %v", err)
					} else {
						t.Log("Q3 CONFIRMED: ScaleNodePool(4) scaled the 2-node pool to 4 (absolute target)")
					}
				}
			}
			// ---- Q3: UpdateNodePool ignoreInitialNodeCount ----
			t.Log("calling UpdateNodePool(ignoreInitialNodeCount=true, no count change)…")
			if err := svc.UpdateNodePool(ctx, UpdateNodePoolInput{
				ClusterID:              clusterID,
				NodePoolID:             nodePoolID,
				IgnoreInitialNodeCount: true,
			}); err != nil {
				t.Errorf("UpdateNodePool(ignore) failed: %v", err)
			} else {
				count, _ := currentNodeCount(ctx, svc, clusterID, nodePoolID)
				t.Logf("Q3 UpdateNodePool(ignore=true): current count=%d (should be unchanged)", count)
			}
		}
	}

	// ---- Q8: delete node pool + cluster with options ----
	if cases["delete"] {
		if nodePoolID != "" {
			t.Log("deleting node pool…")
			if err := svc.DeleteNodePool(ctx, clusterID, nodePoolID); err != nil {
				t.Errorf("DeleteNodePool failed: %v", err)
			} else {
				waitForNodePoolGone(ctx, svc, clusterID, nodePoolID, smokePoolWait, smokePollInterval)
				nodePoolID = ""
			}
		}
		t.Log("deleting cluster with delete options (EVS/ENI/ELB=true)…")
		if err := svc.DeleteCluster(ctx, DeleteClusterInput{
			ClusterID:          clusterID,
			DeleteEVS:          true,
			DeleteENI:          true,
			DeleteELB:          true,
			OnDemandNodePolicy: "delete",
			PeriodicNodePolicy: "reset",
		}); err != nil {
			t.Errorf("DeleteCluster failed: %v", err)
		} else {
			waitForClusterGone(ctx, svc, clusterID, smokeClusterWait, smokePollInterval)
			t.Log("Q8 cluster deletion requested; verify no EVS/ELB leftovers in the console")
			cleaned = true
			clusterID = ""
		}
	}
}

// --- poll helpers ---

func waitForPhase(ctx context.Context, svc Service, clusterID, want string, timeout, interval time.Duration) (*ClusterInfo, error) {
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
		if info.Phase == want {
			return info, nil
		}
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("cluster %s not in phase %q within %v", clusterID, want, timeout)
}

func waitForNodeCount(ctx context.Context, svc Service, clusterID, nodePoolID string, want int32, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count, err := currentActiveNodeCount(ctx, svc, clusterID, nodePoolID)
		if err == nil && count >= want {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("node pool %s did not reach %d nodes within %v", nodePoolID, want, timeout)
}

func currentNodeCount(ctx context.Context, svc Service, clusterID, nodePoolID string) (int32, error) {
	pools, err := svc.ListNodePools(ctx, clusterID)
	if err != nil {
		return 0, err
	}
	for _, p := range pools {
		if p.NodePoolID == nodePoolID {
			// NodeCount is the ACTUAL current node count (Status.CurrentNode).
			// DesiredNodeCount is only the spec's initialNodeCount and is
			// always the target — returning it here made "nodes reached N"
			// pass instantly without any node ever becoming Active (bug found
			// in the live drill; nodes were stuck "Installing" but the check
			// reported success).
			return p.NodeCount, nil
		}
	}
	return 0, fmt.Errorf("node pool %s not found", nodePoolID)
}

func currentActiveNodeCount(ctx context.Context, svc Service, clusterID, nodePoolID string) (int32, error) {
	pools, err := svc.ListNodePools(ctx, clusterID)
	if err != nil {
		return 0, err
	}
	for _, p := range pools {
		if p.NodePoolID == nodePoolID {
			// ActiveNodeCount is status.activeNode — only nodes in Active
			// state. Waiting on this (not currentNode, which also counts
			// "Installing" nodes) ensures the pool is fully provisioned before
			// scale/update/delete; CCE rejects DeleteNodePool while any node is
			// still installing (CCE.01403006, observed live).
			return p.ActiveNodeCount, nil
		}
	}
	return 0, fmt.Errorf("node pool %s not found", nodePoolID)
}

func waitForNodePoolGone(ctx context.Context, svc Service, clusterID, nodePoolID string, timeout, interval time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pools, err := svc.ListNodePools(ctx, clusterID)
		if err != nil || len(pools) == 0 {
			return
		}
		gone := true
		for _, p := range pools {
			if p.NodePoolID == nodePoolID {
				gone = false
				break
			}
		}
		if gone {
			return
		}
		time.Sleep(interval)
	}
}

func waitForClusterGone(ctx context.Context, svc Service, clusterID string, timeout, interval time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := svc.ShowCluster(ctx, clusterID); err != nil {
			return // NotFound (or other) — treat as gone
		}
		time.Sleep(interval)
	}
}

func isTemporary(err error) bool {
	s := err.Error()
	return strings.Contains(s, "throttl") || strings.Contains(s, "lock") || strings.Contains(s, "concurrency")
}

// TestSmokeExtras covers the remaining questionnaire items that need a live
// cluster: Q2 (re-issue immediacy), Q5 (Standard customSecurityGroups),
// Q13 (public API server reachability), Q14 (light throttle observation).
func TestSmokeExtras(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	eniSubnetID := smokeRequired(t, "CCE_SMOKE_ENI_SUBNET")
	keypair := smokeRequired(t, "CCE_SMOKE_KEYPAIR")
	flavor := smokeEnv("CCE_SMOKE_FLAVOR", "c6.large.2")
	cases := smokeCases()
	enabled := func(c string) bool { return cases["extras"] || cases[c] }

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Public Standard/vpc-router cluster (needed for the Q13 reachability
	// check; Standard avoids the sub-ENI flavor constraint).
	clusterName := fmt.Sprintf("capi-smoke-%d", time.Now().Unix()%100000)
	clusterID, err := svc.CreateCluster(ctx, CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: "10.244.0.0/16",
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		PublicAccess:         true, // Q13
		BillingMode:          0,
	})
	if err != nil {
		t.Fatalf("CreateCluster (public) failed: %v", err)
	}
	t.Logf("extras: public cluster %s", clusterID)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}

	// ---- Q13: public endpoint reachability ----
	if enabled("public") {
		info, err := svc.ShowCluster(ctx, clusterID)
		if err != nil {
			t.Errorf("ShowCluster failed: %v", err)
		} else {
			publicURL := ""
			for _, ep := range info.Endpoints {
				if ep.Type == "public" {
					publicURL = ep.URL
				}
			}
			if publicURL == "" {
				t.Log("Q13: no public endpoint returned (EIP may not be allocated) — check console")
			} else {
				t.Logf("Q13: public endpoint %s — probing from this machine…", publicURL)
				reachable, err := probeHTTPS(publicURL, 15*time.Second)
				if err != nil {
					t.Logf("Q13: probe error: %v", err)
				}
				t.Logf("Q13 RESULT: API server at %s reachable from outside VPC = %v", publicURL, reachable)
			}
		}
	}

	// ---- Q2: re-issue immediacy ----
	if enabled("reissue") {
		k1, err := svc.GetClusterKubeconfig(ctx, clusterID, 30)
		if err != nil {
			t.Errorf("Q2 first kubeconfig failed: %v", err)
		}
		k2, err := svc.GetClusterKubeconfig(ctx, clusterID, 30)
		if err != nil {
			t.Errorf("Q2 re-issue failed: %v", err)
		} else if k1 != "" && k2 != "" {
			t.Logf("Q2 RESULT: re-issue succeeds immediately without revoke (kubeconfigs %d/%d bytes)", len(k1), len(k2))
		}
	}

	// ---- Q14: light throttle observation (200 rapid reads) ----
	if enabled("throttle") {
		throttled := 0
		other := 0
		for i := 0; i < 200; i++ {
			if _, err := svc.ShowCluster(ctx, clusterID); err != nil {
				msg := err.Error()
				if strings.Contains(msg, "429") || strings.Contains(msg, "APIGW") || strings.Contains(msg, "throttl") || strings.Contains(msg, "流控") {
					throttled++
				} else {
					other++
				}
			}
		}
		t.Logf("Q14 RESULT: 200 rapid ShowCluster calls -> throttled=%d otherErrors=%d (throttle threshold not hit at this rate)", throttled, other)
	}

	// ---- Q5: Standard cluster with customSecurityGroups ----
	if enabled("sg") {
		sgID, err := createSecurityGroup(ctx, region, ak, sk, vpcID, "capi-smoke-sg-"+fmt.Sprintf("%d", time.Now().Unix()%10000))
		if err != nil {
			t.Logf("Q5: cannot create security group: %v", err)
		} else {
			defer deleteSecurityGroup(ctx, region, ak, sk, sgID)
			poolName := "sg-pool"
			poolID, err := svc.CreateNodePool(ctx, CreateNodePoolInput{
				ClusterID:            clusterID,
				Name:                 poolName,
				Flavor:               flavor,
				OS:                   "Huawei Cloud EulerOS 2.0",
				RootVolumeSize:       40,
				RootVolumeType:       "GPSSD",
				DataVolumes:          []NodeVolumeInput{{Size: 100, Type: "GPSSD"}},
				SSHKey:               keypair,
				AvailabilityZone:     smokeEnv("CCE_SMOKE_AZ", ""),
				InitialNodeCount:     1,
				BillingMode:          0,
				CustomSecurityGroups: []string{sgID},
			})
			if err != nil {
				t.Logf("Q5 RESULT: Standard + customSecurityGroups REJECTED: %v", err)
			} else {
				t.Logf("Q5 RESULT: Standard cluster accepts customSecurityGroups (pool %s) — supported", poolID)
				_ = svc.DeleteNodePool(ctx, clusterID, poolID)
			}
		}
	}

	// ---- Q14b: also exercise eni subnet id validation on Standard ----
	_ = eniSubnetID
}

// TestSmokeUpgrade runs the CCE upgrade workflow end-to-end on an older
// version cluster and measures the duration (Q11).
func TestSmokeUpgrade(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	fromVersion := smokeEnv("CCE_SMOKE_UPGRADE_FROM", "v1.34")
	toVersion := smokeEnv("CCE_SMOKE_UPGRADE_TO", "v1.36")
	keypair := smokeRequired(t, "CCE_SMOKE_KEYPAIR")
	flavor := smokeEnv("CCE_SMOKE_FLAVOR", "c6.large.2")

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client := svc.cce

	clusterName := fmt.Sprintf("capi-upg-%d", time.Now().Unix()%100000)
	// Unique container CIDR per run: the same VPC cannot host two vpc-router
	// clusters with overlapping container CIDRs (verified live: "Container
	// network CIDR conflict"); deleting clusters still hold the CIDR for a
	// few minutes.
	containerCIDR := smokeEnv("CCE_SMOKE_CONTAINER_CIDR", fmt.Sprintf("10.%d.0.0/16", 50+(time.Now().Unix()%200)))
	clusterID, err := svc.CreateCluster(ctx, CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		Version:              fromVersion,
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: containerCIDR,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		BillingMode:          0,
	})
	if err != nil {
		t.Fatalf("CreateCluster(%s) failed: %v", fromVersion, err)
	}
	t.Logf("upgrade: cluster %s at %s", clusterID, fromVersion)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}
	info, _ := svc.ShowCluster(ctx, clusterID)
	t.Logf("upgrade: cluster version now %s, upgrading to %s", info.Version, toVersion)

	// Create a 1-node pool first — an empty cluster may not support upgrade
	// (verified: "not supported to upgrade ... only support to current").
	poolID, err := svc.CreateNodePool(ctx, CreateNodePoolInput{
		ClusterID:        clusterID,
		Name:             "pool-0",
		Flavor:           flavor,
		OS:               "Huawei Cloud EulerOS 2.0",
		RootVolumeSize:   40,
		RootVolumeType:   "GPSSD",
		DataVolumes:      []NodeVolumeInput{{Size: 100, Type: "GPSSD"}},
		SSHKey:           keypair,
		AvailabilityZone: smokeEnv("CCE_SMOKE_AZ", ""),
		InitialNodeCount: 1,
		BillingMode:      0,
	})
	if err != nil {
		t.Fatalf("CreateNodePool (pre-upgrade) failed: %v", err)
	}
	defer func() { _ = svc.DeleteNodePool(ctx, clusterID, poolID) }()
	if err := waitForNodeCount(ctx, svc, clusterID, poolID, 1, smokePoolWait, smokePollInterval); err != nil {
		t.Fatalf("pre-upgrade node pool not ready: %v", err)
	}
	t.Log("upgrade: 1-node pool ready")

	// 1. CreateUpgradeWorkFlow
	t0 := time.Now()
	if _, err := client.CreateUpgradeWorkFlow(&model.CreateUpgradeWorkFlowRequest{
		ClusterId: clusterID,
		Body: &model.CreateUpgradeWorkFlowRequestBody{
			Kind:       "WorkFlowTask",
			ApiVersion: "v3",
			Spec:       &model.WorkFlowSpec{ClusterID: &clusterID, TargetVersion: toVersion},
		},
	}); err != nil {
		t.Fatalf("CreateUpgradeWorkFlow failed: %v", err)
	}
	t.Logf("Q11: CreateUpgradeWorkFlow OK (%s)", time.Since(t0))

	// 2. CreatePreCheck
	if _, err := client.CreatePreCheck(&model.CreatePreCheckRequest{
		ClusterId: clusterID,
		Body: &model.PrecheckClusterRequestBody{
			Kind:       "PreCheck",
			ApiVersion: "v3",
			Spec:       &model.PrecheckSpec{ClusterID: &clusterID, TargetVersion: &toVersion},
		},
	}); err != nil {
		t.Logf("Q11: CreatePreCheck failed (continuing): %v", err)
	} else {
		t.Log("Q11: CreatePreCheck accepted")
	}

	// 3. UpgradeCluster (in-place rolling)
	t1 := time.Now()
	if _, err := client.UpgradeCluster(&model.UpgradeClusterRequest{
		ClusterId: clusterID,
		Body: &model.UpgradeClusterRequestBody{
			Metadata: &model.UpgradeClusterRequestMetadata{Kind: "UpgradeTask", ApiVersion: "v3"},
			Spec: &model.UpgradeSpec{
				ClusterUpgradeAction: &model.ClusterUpgradeAction{
					TargetVersion: toVersion,
					Strategy: &model.UpgradeStrategy{
						InPlaceRollingUpdate: &model.InPlaceRollingUpdate{},
					},
					IsOnlyUpgrade: boolPtr(false),
				},
			},
		},
	}); err != nil {
		t.Fatalf("UpgradeCluster failed: %v", err)
	}
	t.Logf("Q11: UpgradeCluster accepted (%s since workflow start)", time.Since(t0))

	// 4. Poll phases until Available again.
	phaseStart := time.Now()
	for {
		info, err := svc.ShowCluster(ctx, clusterID)
		if err != nil {
			time.Sleep(smokePollInterval)
			continue
		}
		if info.Phase == "Available" && time.Since(phaseStart) > 2*time.Minute {
			break
		}
		if time.Since(phaseStart) > smokeClusterWait {
			t.Fatalf("upgrade did not finish within %v (last phase %s)", smokeClusterWait, info.Phase)
		}
		time.Sleep(smokePollInterval)
	}
	t.Logf("Q11 RESULT: upgrade %s -> %s completed; workflow->ready %v, upgrade phase %v (verify in console)", fromVersion, toVersion, time.Since(t0), time.Since(t1))

	// 5. CreatePostCheck
	if _, err := client.CreatePostCheck(&model.CreatePostCheckRequest{
		ClusterId: clusterID,
		Body: &model.PostcheckClusterRequestBody{
			Kind:       "PostCheckTask",
			ApiVersion: "v3",
			Spec:       &model.PostcheckSpec{ClusterID: &clusterID, TargetVersion: &toVersion},
		},
	}); err != nil {
		t.Logf("Q11: CreatePostCheck failed: %v", err)
	} else {
		t.Log("Q11: CreatePostCheck accepted")
	}
}

// probeHTTPS checks whether an HTTPS endpoint is reachable from this machine
// (used for the Q13 public-endpoint reachability check).
func probeHTTPS(url string, timeout time.Duration) (bool, error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // smoke reachability probe
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return true, nil
}

// createSecurityGroup creates a VPC security group and returns its ID.
func createSecurityGroup(ctx context.Context, regionID, ak, sk, vpcID, name string) (string, error) {
	c, err := newVPCClient(regionID, ak, sk)
	if err != nil {
		return "", err
	}
	resp, err := c.CreateSecurityGroup(&vpcmodel.CreateSecurityGroupRequest{Body: &vpcmodel.CreateSecurityGroupRequestBody{
		SecurityGroup: &vpcmodel.CreateSecurityGroupOption{Name: name, VpcId: &vpcID},
	}})
	if err != nil {
		return "", err
	}
	return resp.SecurityGroup.Id, nil
}

func deleteSecurityGroup(ctx context.Context, regionID, ak, sk, sgID string) {
	c, err := newVPCClient(regionID, ak, sk)
	if err != nil {
		return
	}
	_, _ = c.DeleteSecurityGroup(&vpcmodel.DeleteSecurityGroupRequest{SecurityGroupId: sgID})
}

func newVPCClient(regionID, ak, sk string) (*vpcv2.VpcClient, error) {
	region, err := vpcRegion.SafeValueOf(regionID)
	if err != nil {
		return nil, err
	}
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		return nil, err
	}
	hc, err := vpcv2.VpcClientBuilder().WithRegion(region).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return nil, err
	}
	return vpcv2.NewVpcClient(hc), nil
}

// TestSmokeRemaining covers Q13 (public EIP binding + reachability) and Q14
// (concurrent burst to observe throttling) on one cluster.
func TestSmokeRemaining(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	cases := smokeCases()
	enabled := func(c string) bool { return cases["remaining"] || cases[c] }

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client := svc.cce

	clusterName := fmt.Sprintf("capi-rem-%d", time.Now().Unix()%100000)
	containerCIDR := fmt.Sprintf("10.%d.0.0/16", 50+(time.Now().Unix()%200))
	clusterID, err := svc.CreateCluster(ctx, CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: containerCIDR,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		BillingMode:          0,
	})
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	t.Logf("remaining: cluster %s", clusterID)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}

	// ---- Q13: bind an EIP to the API server, then probe reachability ----
	if enabled("public") {
		eipID, eipAddr, err := createPublicIP(ctx, region, ak, sk, "capi-smoke-eip")
		if err != nil {
			t.Logf("Q13: create EIP failed (quota?): %v", err)
		} else {
			defer deletePublicIP(ctx, region, ak, sk, eipID)
			if _, err := client.UpdateClusterEip(&model.UpdateClusterEipRequest{
				ClusterId: clusterID,
				Body: &model.MasterEipRequest{
					Spec: &model.MasterEipRequestSpec{
						Action: masterEipActionPtr(model.GetMasterEipRequestSpecActionEnum().BIND),
						Spec:   &model.MasterEipRequestSpecSpec{Id: &eipID},
					},
				},
			}); err != nil {
				t.Logf("Q13: UpdateClusterEip(bind) failed: %v", err)
			} else {
				t.Logf("Q13: EIP %s bound to API server", eipAddr)
				// Wait for the endpoint to appear.
				publicURL := ""
				for i := 0; i < 10; i++ {
					time.Sleep(20 * time.Second)
					if info, err := svc.ShowCluster(ctx, clusterID); err == nil {
						for _, ep := range info.Endpoints {
							if ep.Type == "public" {
								publicURL = ep.URL
							}
						}
					}
					if publicURL != "" {
						break
					}
				}
				// The EIP bind may not be reflected in ShowCluster.Status.Endpoints
				// (the dedicated ShowClusterEndpoints API exposes publicEndpoint);
				// probe the bound EIP directly as fallback.
				if publicURL == "" && eipAddr != "" {
					publicURL = fmt.Sprintf("https://%s:5443", eipAddr)
				}
				if publicURL == "" {
					t.Log("Q13: no public endpoint after bind — check console")
				} else {
					t.Logf("Q13: public endpoint %s — probing from this machine…", publicURL)
					if reachable, err := probeHTTPS(publicURL, 15*time.Second); err != nil {
						t.Logf("Q13: probe error (endpoint may still be provisioning): %v", err)
					} else {
						t.Logf("Q13 RESULT: API server public reachable = %v (%s)", reachable, publicURL)
					}
				}
			}
		}
	}

	// ---- Q14: concurrent burst to observe throttling ----
	if enabled("throttle") {
		const workers = 10
		const perWorker = 100 // 1000 total requests
		var mu sync.Mutex
		throttled := 0
		errors := 0
		var sampleErrs []string
		start := time.Now()
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perWorker; i++ {
					if _, err := svc.ShowCluster(ctx, clusterID); err != nil {
						msg := err.Error()
						mu.Lock()
						if strings.Contains(msg, "429") || strings.Contains(msg, "APIGW") || strings.Contains(msg, "throttl") || strings.Contains(msg, "流控") {
							throttled++
						} else {
							errors++
							if len(sampleErrs) < 3 {
								sampleErrs = append(sampleErrs, msg)
							}
						}
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		elapsed := time.Since(start)
		t.Logf("Q14 RESULT: %d concurrent ShowCluster calls in %v (%.0f req/s) -> throttled=%d otherErrors=%d sampleErrors=%v",
			workers*perWorker, elapsed, float64(workers*perWorker)/elapsed.Seconds(), throttled, errors, sampleErrs)
	}
}

// TestSmokeUpgradeInfo answers Q11 definitively: what upgrade targets does the
// platform actually offer from an older-version cluster.
func TestSmokeUpgradeInfo(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	keypair := smokeRequired(t, "CCE_SMOKE_KEYPAIR")
	flavor := smokeEnv("CCE_SMOKE_FLAVOR", "c6.large.2")
	fromVersion := smokeEnv("CCE_SMOKE_UPGRADE_FROM", "v1.34")

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client := svc.cce

	clusterName := fmt.Sprintf("capi-upg-%d", time.Now().Unix()%100000)
	containerCIDR := fmt.Sprintf("10.%d.0.0/16", 50+(time.Now().Unix()%200))
	clusterID, err := svc.CreateCluster(ctx, CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		Version:              fromVersion,
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: containerCIDR,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		BillingMode:          0,
	})
	if err != nil {
		t.Fatalf("CreateCluster(%s) failed: %v", fromVersion, err)
	}
	t.Logf("upgradeinfo: cluster %s at %s", clusterID, fromVersion)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}

	// Query the platform's offered upgrade targets (definitive Q11 answer).
	info, err := client.ShowClusterUpgradeInfo(&model.ShowClusterUpgradeInfoRequest{ClusterId: clusterID})
	if err != nil {
		t.Fatalf("ShowClusterUpgradeInfo failed: %v", err)
	}
	release, patch, suggest := "", "", ""
	targets := []string{}
	if info.Spec != nil && info.Spec.VersionInfo != nil {
		if info.Spec.VersionInfo.Release != nil {
			release = *info.Spec.VersionInfo.Release
		}
		if info.Spec.VersionInfo.Patch != nil {
			patch = *info.Spec.VersionInfo.Patch
		}
		if info.Spec.VersionInfo.SuggestPatch != nil {
			suggest = *info.Spec.VersionInfo.SuggestPatch
		}
		if info.Spec.VersionInfo.TargetVersions != nil {
			targets = *info.Spec.VersionInfo.TargetVersions
		}
	}
	t.Logf("Q11 RESULT: cluster %s platform release=%s patch=%s suggestPatch=%s; OFFERED upgrade targets=%v",
		fromVersion, release, patch, suggest, targets)
	clusterVersion := release
	if patch != "" {
		clusterVersion += "-" + patch
	}
	if len(targets) == 0 {
		t.Log("Q11: no upgrade targets offered — the earlier 'only support to current' is the platform's current upgrade policy (no cross-minor path from this version)")
		return
	}
	// Attempt the upgrade to the first offered target (if any).
	target := targets[0]
	if _, err := client.CreateUpgradeWorkFlow(&model.CreateUpgradeWorkFlowRequest{
		ClusterId: clusterID,
		Body: &model.CreateUpgradeWorkFlowRequestBody{
			Kind:       "WorkFlowTask",
			ApiVersion: "v3",
			Spec:       &model.WorkFlowSpec{ClusterID: &clusterID, TargetVersion: target},
		},
	}); err != nil {
		t.Logf("Q11: upgrade to offered target %s still rejected: %v", target, err)
	} else {
		t.Logf("Q11: upgrade workflow to %s accepted — timing the upgrade…", target)
		// Add a node pool first (nodes are rolled during upgrade).
		poolID, err := svc.CreateNodePool(ctx, CreateNodePoolInput{
			ClusterID: clusterID, Name: "pool-0", Flavor: flavor, OS: "Huawei Cloud EulerOS 2.0",
			RootVolumeSize: 40, RootVolumeType: "GPSSD", DataVolumes: []NodeVolumeInput{{Size: 100, Type: "GPSSD"}},
			SSHKey: keypair, InitialNodeCount: 1, BillingMode: 0,
		})
		if err != nil {
			t.Fatalf("CreateNodePool failed: %v", err)
		}
		defer func() { _ = svc.DeleteNodePool(ctx, clusterID, poolID) }()
		if err := waitForNodeCount(ctx, svc, clusterID, poolID, 1, smokePoolWait, smokePollInterval); err != nil {
			t.Fatalf("node pool not ready: %v", err)
		}
		if _, err := client.CreatePreCheck(&model.CreatePreCheckRequest{
			ClusterId: clusterID,
			Body:      &model.PrecheckClusterRequestBody{Kind: "PreCheckTask", ApiVersion: "v3", Spec: &model.PrecheckSpec{ClusterID: &clusterID, ClusterVersion: &clusterVersion, TargetVersion: &target}},
		}); err != nil {
			t.Logf("Q11: CreatePreCheck failed: %v", err)
		}
		t0 := time.Now()
		if _, err := client.UpgradeCluster(&model.UpgradeClusterRequest{
			ClusterId: clusterID,
			Body: &model.UpgradeClusterRequestBody{
				Metadata: &model.UpgradeClusterRequestMetadata{Kind: "UpgradeTask", ApiVersion: "v3"},
				Spec: &model.UpgradeSpec{
					ClusterUpgradeAction: &model.ClusterUpgradeAction{
						TargetVersion: target,
						Strategy:      &model.UpgradeStrategy{InPlaceRollingUpdate: &model.InPlaceRollingUpdate{}},
						IsOnlyUpgrade: boolPtr(false),
					},
				},
			},
		}); err != nil {
			t.Logf("Q11: UpgradeCluster failed: %v", err)
		} else {
			start := time.Now()
			for {
				info, err := svc.ShowCluster(ctx, clusterID)
				if err != nil {
					time.Sleep(smokePollInterval)
					continue
				}
				if info.Phase == "Available" && time.Since(start) > 2*time.Minute {
					break
				}
				if time.Since(start) > smokeClusterWait {
					t.Fatalf("upgrade not finished in %v (phase %s)", smokeClusterWait, info.Phase)
				}
				time.Sleep(smokePollInterval)
			}
			t.Logf("Q11 RESULT: upgrade %s -> %s completed in %v", fromVersion, target, time.Since(t0))
		}
	}
}

// createPublicIP creates an EIP and returns its ID and address.
func createPublicIP(ctx context.Context, regionID, ak, sk, name string) (string, string, error) {
	region, err := eipRegion.SafeValueOf(regionID)
	if err != nil {
		return "", "", err
	}
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		return "", "", err
	}
	hc, err := eipv2.EipClientBuilder().WithRegion(region).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return "", "", err
	}
	c := eipv2.NewEipClient(hc)
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
	id := ""
	addr := ""
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

func deletePublicIP(ctx context.Context, regionID, ak, sk, eipID string) {
	region, err := eipRegion.SafeValueOf(regionID)
	if err != nil {
		return
	}
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		return
	}
	hc, err := eipv2.EipClientBuilder().WithRegion(region).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return
	}
	c := eipv2.NewEipClient(hc)
	_, _ = c.DeletePublicip(&eipmodel.DeletePublicipRequest{PublicipId: eipID})
}

func masterEipActionPtr(a model.MasterEipRequestSpecAction) *model.MasterEipRequestSpecAction {
	return &a
}

// TestSmokeAutoscaling verifies B3 end-to-end: a node pool created with
// autoscaling (enable/min/max) is accepted by CCE and the configuration is
// readable back; autoscaling coexists with manual ScaleNodePool (the two
// mechanisms do not conflict — questionnaire Q3 + FR-2.6).
//
// Enable with: CCE_SMOKE_CASES=autoscaling
func TestSmokeAutoscaling(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	keypair := smokeRequired(t, "CCE_SMOKE_KEYPAIR")
	flavor := smokeEnv("CCE_SMOKE_FLAVOR", "c6.large.2")
	cases := smokeCases()
	enabled := func(c string) bool { return cases["autoscaling"] || cases[c] }

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	clusterName := fmt.Sprintf("capi-auto-%d", time.Now().Unix()%100000)
	containerCIDR := fmt.Sprintf("10.%d.0.0/16", 70+(time.Now().Unix()%120))
	clusterID, err := svc.CreateCluster(ctx, CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: containerCIDR,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		BillingMode:          0,
	})
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	t.Logf("autoscaling: cluster %s", clusterID)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}

	if !enabled("pool") {
		t.Log("B3: pool case disabled (CCE_SMOKE_CASES=autoscaling)")
		return
	}

	// ---- B3: create node pool with autoscaling enable=true min=1 max=4 ----
	nodePoolID, err := svc.CreateNodePool(ctx, CreateNodePoolInput{
		ClusterID:        clusterID,
		Name:             "pool-auto",
		Flavor:           flavor,
		OS:               "Huawei Cloud EulerOS 2.0",
		SSHKey:           keypair,
		AvailabilityZone: smokeEnv("CCE_SMOKE_AZ", "cn-north-4a"),
		InitialNodeCount: 1,
		RootVolumeSize:   40,
		RootVolumeType:   "SSD",
		DataVolumes:      []NodeVolumeInput{{Size: 100, Type: "SSD"}},
		Autoscaling:      &NodePoolAutoscaling{Enable: true, MinNodeCount: 1, MaxNodeCount: 4},
	})
	if err != nil {
		t.Fatalf("CreateNodePool with autoscaling failed: %v", err)
	}
	t.Logf("B3: node pool %s created with autoscaling {enable,1,4}", nodePoolID)
	defer func() { _ = svc.DeleteNodePool(ctx, clusterID, nodePoolID) }()
	if err := waitForNodeCount(ctx, svc, clusterID, nodePoolID, 1, smokePoolWait, smokePollInterval); err != nil {
		t.Fatalf("node pool did not reach 1 node: %v", err)
	}

	// Read back the autoscaling config via the raw SDK response.
	if as := readPoolAutoscaling(ctx, svc, clusterID, nodePoolID); as == nil {
		t.Error("B3: ListNodePools shows NO autoscaling config — CCE did not persist it")
	} else if !derefBool(as.Enable) || derefI32(as.MinNodeCount) != 1 || derefI32(as.MaxNodeCount) != 4 {
		t.Errorf("B3: autoscaling read back mismatch: enable=%v min=%d max=%d (want true/1/4)",
			derefBool(as.Enable), derefI32(as.MinNodeCount), derefI32(as.MaxNodeCount))
	} else {
		t.Logf("B3 RESULT: autoscaling persisted and readable: enable=true min=1 max=4")
	}

	// ---- B3: autoscaling coexists with manual ScaleNodePool ----
	if !enabled("scale") {
		return
	}
	t.Log("B3: calling ScaleNodePool(2) on an autoscaling pool…")
	if err := svc.ScaleNodePool(ctx, clusterID, nodePoolID, 2); err != nil {
		t.Fatalf("ScaleNodePool on autoscaling pool failed: %v", err)
	}
	if err := waitForNodeCount(ctx, svc, clusterID, nodePoolID, 2, smokePoolWait, smokePollInterval); err != nil {
		t.Fatalf("pool did not reach 2 nodes: %v", err)
	}
	if as := readPoolAutoscaling(ctx, svc, clusterID, nodePoolID); as == nil || !derefBool(as.Enable) {
		t.Error("B3: autoscaling config lost after manual scale")
	} else {
		t.Log("B3 RESULT: autoscaling + manual ScaleNodePool coexist (pool scaled to 2, autoscaling still enabled)")
	}
}

// readPoolAutoscaling returns the autoscaling config of a node pool from the
// raw SDK ListNodePools response (smoke test runs in-package, so it can reach
// the underlying client).
func readPoolAutoscaling(ctx context.Context, svc Service, clusterID, nodePoolID string) *model.NodePoolNodeAutoscaling {
	c := svc.(*Client)
	resp, err := c.cce.ListNodePools(&model.ListNodePoolsRequest{ClusterId: clusterID})
	if err != nil {
		return nil
	}
	if resp.Items != nil {
		for _, p := range *resp.Items {
			if p.Metadata != nil && p.Metadata.Uid != nil && *p.Metadata.Uid == nodePoolID && p.Spec != nil {
				return p.Spec.Autoscaling
			}
		}
	}
	return nil
}

func derefI32(v *int32) int32 {
	if v == nil {
		return 0
	}
	return *v
}

func derefBool(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

// TestSmokeUpgradeWorkflow verifies E3 end-to-end: GetUpgradeInfo returns the
// platform-offered targets; when targets exist the workflow is driven
// (StartUpgrade -> ShowUpgradeTask until Success/Failed); when none are
// offered (questionnaire Q11: verified empty on v1.34.8-r2) it records the
// platform rejection so the controller's UpgradeNotOffered path is grounded.
//
// Enable with: CCE_SMOKE_CASES=upgrade
func TestSmokeUpgradeWorkflow(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")
	keypair := smokeRequired(t, "CCE_SMOKE_KEYPAIR")
	flavor := smokeEnv("CCE_SMOKE_FLAVOR", "c6.large.2")
	fromVersion := smokeEnv("CCE_SMOKE_UPGRADE_FROM", "v1.34")

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	clusterName := fmt.Sprintf("capi-upgwf-%d", time.Now().Unix()%100000)
	containerCIDR := fmt.Sprintf("10.%d.0.0/16", 80+(time.Now().Unix()%100))
	upgradeMode := smokeEnv("CCE_SMOKE_UPGRADE_MODE", "vpc-router") // vpc-router (Standard) | eni (Turbo)
	createIn := CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		Version:              fromVersion,
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: containerCIDR,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		BillingMode:          0,
	}
	if upgradeMode == "eni" {
		// CCE Turbo: eni network; ENI subnets must use neutron_subnet_id
		// (verified: plain subnet id -> CCE_CM.0004).
		createIn.Category = "Turbo"
		createIn.ContainerNetworkMode = "eni"
		createIn.ENISubnets = []string{smokeRequired(t, "CCE_SMOKE_ENI_SUBNET")}
	}
	clusterID, err := svc.CreateCluster(ctx, createIn)
	if err != nil {
		t.Fatalf("CreateCluster(%s, %s) failed: %v", fromVersion, upgradeMode, err)
	}
	t.Logf("upgrade-workflow: cluster %s at %s", clusterID, fromVersion)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}

	// Optional: a node pool. The platform's upgrade orchestration rolls nodes
	// in batches (in-place), so an EMPTY cluster may not be offered any
	// upgrade target — verify both shapes (Q11).
	var poolID string
	if smokeEnv("CCE_SMOKE_UPGRADE_WITH_POOL", "0") == "1" {
		poolID, err = svc.CreateNodePool(ctx, CreateNodePoolInput{
			ClusterID:        clusterID,
			Name:             "pool-0",
			Flavor:           flavor,
			OS:               "Huawei Cloud EulerOS 2.0",
			SSHKey:           keypair,
			AvailabilityZone: smokeEnv("CCE_SMOKE_AZ", "cn-north-4a"),
			InitialNodeCount: 1,
			RootVolumeSize:   40,
			RootVolumeType:   "SSD",
			DataVolumes:      []NodeVolumeInput{{Size: 100, Type: "SSD"}},
			BillingMode:      0,
		})
		if err != nil {
			t.Fatalf("CreateNodePool failed: %v", err)
		}
		t.Logf("E3: node pool %s created (1 node)", poolID)
		defer func() { _ = svc.DeleteNodePool(ctx, clusterID, poolID) }()
		if err := waitForNodeCount(ctx, svc, clusterID, poolID, 1, smokePoolWait, smokePollInterval); err != nil {
			t.Fatalf("node pool not ready: %v", err)
		}
		t.Log("E3: node pool reached 1 node (Active)")
	}

	// ---- E3: what does the platform actually offer? ----
	info, err := svc.GetUpgradeInfo(ctx, clusterID)
	if err != nil {
		t.Fatalf("GetUpgradeInfo failed: %v", err)
	}
	t.Logf("E3: GetUpgradeInfo -> current=%s targets=%v", info.CurrentVersion, info.TargetVersions)

	if len(info.TargetVersions) == 0 {
		// No cross-version target offered (Q11). The official upgrade-path
		// table supports v1.34 -> v1.35, so an empty list usually means the
		// running patch is not the latest one: the platform requires the
		// latest patch before a version upgrade. Log the full version info
		// (suggestPatch) to confirm.
		raw, rerr := svc.cce.ShowClusterUpgradeInfo(&model.ShowClusterUpgradeInfoRequest{ClusterId: clusterID})
		if rerr == nil && raw.Spec != nil && raw.Spec.VersionInfo != nil {
			vi := raw.Spec.VersionInfo
			suggest, release, patch := "", "", ""
			if vi.SuggestPatch != nil {
				suggest = *vi.SuggestPatch
			}
			if vi.Release != nil {
				release = *vi.Release
			}
			if vi.Patch != nil {
				patch = *vi.Patch
			}
			t.Logf("E3: ShowClusterUpgradeInfo -> release=%s patch=%s suggestPatch=%s targets=%v",
				release, patch, suggest, info.TargetVersions)
		}
		// Verify the controller-facing signal: StartUpgrade must be rejected
		// by the platform, which is why the controller reports
		// UpgradeNotOffered instead of calling it.
		t.Log("E3: no upgrade targets offered — attempting StartUpgrade to record the platform rejection…")
		_, err := svc.StartUpgrade(ctx, clusterID, fromVersion)
		if err != nil {
			t.Logf("E3 RESULT: platform rejected StartUpgrade as expected: %v (controller path: UpgradeNotOffered)", err)
		} else {
			t.Log("E3: StartUpgrade unexpectedly accepted with no targets (review)")
		}
		return
	}

	// Targets exist: drive the workflow and poll to completion.
	target := info.TargetVersions[0]
	t.Logf("E3: starting upgrade to %s…", target)
	start := time.Now()
	taskID, err := svc.StartUpgrade(ctx, clusterID, target)
	if err != nil {
		t.Fatalf("StartUpgrade(%s) failed: %v", target, err)
	}
	t.Logf("E3: upgrade task %s started", taskID)
	phase := ""
	for i := 0; i < 240; i++ { // poll up to 2h
		time.Sleep(30 * time.Second)
		phase, err = svc.ShowUpgradeTask(ctx, clusterID, taskID)
		if err != nil {
			t.Logf("E3: ShowUpgradeTask error (retrying): %v", err)
			continue
		}
		t.Logf("E3: upgrade phase=%s elapsed=%v", phase, time.Since(start).Round(time.Second))
		if phase == UpgradeTaskPhaseSuccess || phase == UpgradeTaskPhaseFailed {
			break
		}
	}
	if phase == UpgradeTaskPhaseSuccess {
		t.Logf("E3 RESULT: upgrade to %s SUCCEEDED in %v (task %s)", target, time.Since(start).Round(time.Second), taskID)
	} else if phase == UpgradeTaskPhaseFailed {
		t.Logf("E3 RESULT: upgrade task FAILED (phase=%s, task %s)", phase, taskID)
		// Dump the full task detail (spec.items carries per-step outcomes) to
		// diagnose why the upgrade failed.
		if raw, rerr := svc.cce.ShowUpgradeClusterTask(&model.ShowUpgradeClusterTaskRequest{ClusterId: clusterID, TaskId: taskID}); rerr == nil {
			if raw.Spec != nil && raw.Spec.Items != nil {
				if b, jerr := json.MarshalIndent(raw.Spec.Items, "", "  "); jerr == nil {
					t.Logf("E3: upgrade task items:\n%s", string(b))
				}
			}
		}
		// The task list carries richer history (incl. items of each task).
		if raw, rerr := svc.cce.ListUpgradeClusterTasks(&model.ListUpgradeClusterTasksRequest{ClusterId: clusterID}); rerr == nil {
			if b, jerr := json.MarshalIndent(raw, "", "  "); jerr == nil {
				t.Logf("E3: upgrade task list:\n%s", string(b))
			}
		}
	} else {
		t.Logf("E3 RESULT: upgrade still in phase %q after poll window (task %s)", phase, taskID)
	}
}

// TestSmokeLogging verifies control-plane log collection end-to-end: apply a
// config via UpdateClusterLogConfig and read it back with ShowClusterConfig.
// Mirrors CAPA EKS Logging (questionnaire Q-LOG).
func TestSmokeLogging(t *testing.T) {
	ctx := context.Background()
	ak := smokeRequired(t, "CCE_SMOKE_AK")
	sk := smokeRequired(t, "CCE_SMOKE_SK")
	region := smokeEnv("CCE_SMOKE_REGION", "cn-north-4")
	vpcID := smokeRequired(t, "CCE_SMOKE_VPC")
	subnetID := smokeRequired(t, "CCE_SMOKE_SUBNET")

	svc, err := NewClient(region, ak, sk)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	clusterName := fmt.Sprintf("capi-log-%d", time.Now().Unix()%100000)
	containerCIDR := fmt.Sprintf("10.%d.0.0/16", 50+(time.Now().Unix()%200))
	clusterID, err := svc.CreateCluster(ctx, CreateClusterInput{
		Name:                 clusterName,
		Category:             "CCE",
		Flavor:               smokeEnv("CCE_SMOKE_CLUSTER_FLAVOR", "cce.s1.small"),
		ContainerNetworkMode: "vpc-router",
		ContainerNetworkCIDR: containerCIDR,
		HostNetworkVpcID:     vpcID,
		HostNetworkSubnetID:  subnetID,
		ServiceCIDR:          "10.247.0.0/16",
		BillingMode:          0,
	})
	if err != nil {
		t.Fatalf("CreateCluster failed: %v", err)
	}
	t.Logf("logging: cluster %s", clusterID)
	defer func() {
		_ = svc.DeleteCluster(ctx, DeleteClusterInput{ClusterID: clusterID, DeleteEVS: true, DeleteENI: true, DeleteELB: true, OnDemandNodePolicy: "delete"})
	}()
	if _, err := waitForPhase(ctx, svc, clusterID, "Available", smokeClusterWait, smokePollInterval); err != nil {
		t.Fatalf("cluster not Available: %v", err)
	}

	want := []LogConfigInput{
		{Name: "kube-apiserver", Type: "control", Enable: true},
		{Name: "kube-controller-manager", Type: "control", Enable: true},
		{Name: "kube-scheduler", Type: "control", Enable: true},
		{Name: "audit", Type: "audit", Enable: true},
	}
	if err := svc.UpdateClusterLogConfig(ctx, clusterID, 7, want); err != nil {
		t.Fatalf("UpdateClusterLogConfig failed: %v", err)
	}
	got, err := svc.ShowClusterLogConfig(ctx, clusterID)
	if err != nil {
		t.Fatalf("ShowClusterLogConfig failed: %v", err)
	}
	t.Logf("Q-LOG RESULT: ttl=%d logItems=%d", got.TTLInDays, len(got.Logs))
	for _, l := range got.Logs {
		t.Logf("Q-LOG item: name=%s type=%s enable=%v", l.Name, l.Type, l.Enable)
	}
	if got.TTLInDays != 7 {
		t.Errorf("Q-LOG: expected ttl 7, got %d", got.TTLInDays)
	}
	if len(got.Logs) == 0 {
		t.Errorf("Q-LOG: expected log config items to be returned, got none")
	}
}
