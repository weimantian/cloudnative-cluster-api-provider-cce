/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

// TestOwnedClusterName verifies the owned-tag -> cluster-name extraction.
func TestOwnedClusterName(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want string
	}{
		{name: "owned tag", tags: map[string]string{"cluster-api-provider-cce.cluster.foo": "owned"}, want: "foo"},
		{name: "non-owned value", tags: map[string]string{"cluster-api-provider-cce.cluster.foo": "shared"}, want: ""},
		{name: "unrelated tag", tags: map[string]string{"foo": "bar"}, want: ""},
		{name: "empty", tags: nil, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownedClusterName(tc.tags); got != tc.want {
				t.Errorf("ownedClusterName(%v) = %q, want %q", tc.tags, got, tc.want)
			}
		})
	}
}

// TestGarbageCollectorSweep verifies the orphaned-cluster sweeper: owned-tagged
// CCE clusters whose Cluster CR no longer exists are deleted; tracked clusters
// and non-owned clusters are left alone.
func TestGarbageCollectorSweep(t *testing.T) {
	ctx := context.Background()
	ns := "gc-test"
	createNamespace(t, ns)

	// One Cluster CR that should exist.
	cluster := &clusterv1.Cluster{}
	cluster.Name = "tracked-cluster"
	cluster.Namespace = ns
	cluster.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "CCECluster", Name: "tracked-cluster"}
	if err := k8sClient.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create Cluster CR: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ListClustersFn = func(_ context.Context) ([]cceService.ClusterRef, error) {
		return []cceService.ClusterRef{
			// Orphaned: owned tag, no Cluster CR -> must be deleted.
			{ClusterID: "orphan-1", Name: "orphan-1", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			// Tracked: owned tag, has a Cluster CR -> must be kept.
			{ClusterID: "tracked-cluster", Name: "tracked-cluster", Tags: map[string]string{"cluster-api-provider-cce.cluster.tracked-cluster": "owned"}},
			// Not ours: no owned tag -> must be kept.
			{ClusterID: "someone-elses", Name: "someone-elses", Tags: map[string]string{}},
		}, nil
	}

	gc := &GarbageCollector{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
		Region:   "cn-north-4",
		Interval: 1,
	}
	// The sweep uses controller-default (env) credentials. Set them so the
	// credential resolution succeeds.
	t.Setenv("CLOUD_SDK_AK", "test-ak")
	t.Setenv("CLOUD_SDK_SK", "test-sk")

	gc.sweep(ctx)

	if len(fakeSvc.DeletedClusters) != 1 {
		t.Fatalf("expected exactly 1 deleted orphan, got %d (%+v)", len(fakeSvc.DeletedClusters), fakeSvc.DeletedClusters)
	}
	d := fakeSvc.DeletedClusters[0]
	if d.ClusterID != "orphan-1" {
		t.Errorf("expected orphan-1 to be deleted, got %+v", d)
	}
	if !d.DeleteEVS || !d.DeleteENI || !d.DeleteELB || d.OnDemandNodePolicy != "delete" {
		t.Errorf("expected cascade delete options, got %+v", d)
	}
}

// TestGarbageCollectorSweepEipEvs verifies phase-2: owned-tagged EIP/EVS
// whose Cluster CR is gone are deleted; tracked/non-owned ones are kept.
func TestGarbageCollectorSweepEipEvs(t *testing.T) {
	ctx := context.Background()
	ns := "gc-test-eip-evsv"
	createNamespace(t, ns)

	cluster := &clusterv1.Cluster{}
	cluster.Name = "tracked-cluster"
	cluster.Namespace = ns
	cluster.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "CCECluster", Name: "tracked-cluster"}
	if err := k8sClient.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create Cluster CR: %v", err)
	}

	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ListEipsFn = func(_ context.Context) ([]cceService.EipRef, error) {
		return []cceService.EipRef{
			{ID: "eip-orphan", Address: "1.2.3.4", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			{ID: "eip-tracked", Address: "5.6.7.8", Tags: map[string]string{"cluster-api-provider-cce.cluster.tracked-cluster": "owned"}},
			{ID: "eip-foreign", Address: "9.10.11.12", Tags: map[string]string{}},
		}, nil
	}
	fakeSvc.ListVolumesFn = func(_ context.Context) ([]cceService.VolumeRef, error) {
		return []cceService.VolumeRef{
			{ID: "vol-orphan", Name: "orphan-disk", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			{ID: "vol-foreign", Name: "other-disk", Tags: map[string]string{}},
		}, nil
	}
	fakeSvc.ListVpcsFn = func(_ context.Context) ([]cceService.VpcRef, error) {
		return []cceService.VpcRef{
			{ID: "vpc-orphan", Name: "orphan-vpc", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			{ID: "vpc-foreign", Name: "other-vpc", Tags: map[string]string{}},
		}, nil
	}
	fakeSvc.ListNatGatewaysFn = func(_ context.Context) ([]cceService.NatGatewayRef, error) {
		return []cceService.NatGatewayRef{
			{ID: "nat-orphan", Name: "orphan-nat", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			{ID: "nat-tracked", Name: "tracked-nat", Tags: map[string]string{"cluster-api-provider-cce.cluster.tracked-cluster": "owned"}},
		}, nil
	}

	gc := &GarbageCollector{
		Client: k8sClient,
		ServiceFactory: func(_, _, _ string) (cceService.Service, error) {
			return fakeSvc, nil
		},
		Region:        "cn-north-4",
		Interval:      1,
		ResourceTypes: []string{"eip", "evs", "vpc", "nat"},
	}
	t.Setenv("CLOUD_SDK_AK", "test-ak")
	t.Setenv("CLOUD_SDK_SK", "test-sk")

	gc.sweep(ctx)

	if len(fakeSvc.DeletedEips) != 1 || fakeSvc.DeletedEips[0] != "eip-orphan" {
		t.Errorf("expected only eip-orphan deleted, got %v", fakeSvc.DeletedEips)
	}
	if len(fakeSvc.DeletedVolumes) != 1 || fakeSvc.DeletedVolumes[0] != "vol-orphan" {
		t.Errorf("expected only vol-orphan deleted, got %v", fakeSvc.DeletedVolumes)
	}
	if len(fakeSvc.DeletedVpcs) != 1 || fakeSvc.DeletedVpcs[0] != "vpc-orphan" {
		t.Errorf("expected only vpc-orphan deleted, got %v", fakeSvc.DeletedVpcs)
	}
	if len(fakeSvc.DeletedNatGateways) != 1 || fakeSvc.DeletedNatGateways[0] != "nat-orphan" {
		t.Errorf("expected only nat-orphan deleted (tracked kept), got %v", fakeSvc.DeletedNatGateways)
	}
}
