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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
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
			DataVolumeSize:   100,
			DataVolumeType:   "GPSSD",
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
		count, err := currentNodeCount(ctx, svc, clusterID, nodePoolID)
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
			return p.DesiredNodeCount, nil
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
