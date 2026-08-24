/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
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
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
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
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
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

// TestSkipGCAnnotation verifies the per-cluster opt-out annotation parsing:
// any truthy value (true/yes/1/on, case-insensitive) means opt-out.
func TestSkipGCAnnotation(t *testing.T) {
	cases := []struct {
		name string
		ann  map[string]string
		want bool
	}{
		{name: "missing", ann: nil, want: false},
		{name: "unrelated key", ann: map[string]string{"foo": "true"}, want: false},
		{name: "explicit true", ann: map[string]string{skipGCAnnotationKey: "true"}, want: true},
		{name: "explicit yes", ann: map[string]string{skipGCAnnotationKey: "yes"}, want: true},
		{name: "explicit 1", ann: map[string]string{skipGCAnnotationKey: "1"}, want: true},
		{name: "explicit on", ann: map[string]string{skipGCAnnotationKey: "on"}, want: true},
		{name: "TRUE upper", ann: map[string]string{skipGCAnnotationKey: "TRUE"}, want: true},
		{name: "with spaces", ann: map[string]string{skipGCAnnotationKey: "  on  "}, want: true},
		{name: "false string", ann: map[string]string{skipGCAnnotationKey: "false"}, want: false},
		{name: "empty value", ann: map[string]string{skipGCAnnotationKey: ""}, want: false},
		{name: "garbage value", ann: map[string]string{skipGCAnnotationKey: "maybe"}, want: false},
		{name: "nil cluster", ann: nil, want: false}, // documents nil-safety
	}
	for _, tc := range cases {
		var c *clusterv1.Cluster
		if tc.name != "nil cluster" {
			cl := &clusterv1.Cluster{}
			cl.Annotations = tc.ann
			c = cl
		}
		t.Run(tc.name, func(t *testing.T) {
			if got := skipGCAnnotation(c); got != tc.want {
				t.Errorf("skipGCAnnotation(%v) = %v, want %v", tc.ann, got, tc.want)
			}
		})
	}
}

// TestGarbageCollectorSweepSkipsOptedOutCluster verifies that a Cluster CR
// carrying the skipGC annotation blocks GC of its orphaned cloud resources,
// while non-opted clusters are still cleaned up.
func TestGarbageCollectorSweepSkipsOptedOutCluster(t *testing.T) {
	ctx := context.Background()
	ns := "gc-test-skip"
	createNamespace(t, ns)

	// Opted-out cluster CR.
	opted := &clusterv1.Cluster{}
	opted.Name = "opted-out"
	opted.Namespace = ns
	opted.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "CCECluster", Name: "opted-out"}
	opted.Annotations = map[string]string{skipGCAnnotationKey: "true"}
	if err := k8sClient.Create(ctx, opted); err != nil {
		t.Fatalf("failed to create opted-out Cluster CR: %v", err)
	}

	// Tracked cluster CR (no annotation, normal behaviour).
	tracked := &clusterv1.Cluster{}
	tracked.Name = "tracked"
	tracked.Namespace = ns
	tracked.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "CCECluster", Name: "tracked"}
	if err := k8sClient.Create(ctx, tracked); err != nil {
		t.Fatalf("failed to create tracked Cluster CR: %v", err)
	}

	// Orphan cluster CR (no CR; GC candidates).
	fakeSvc := fakes.NewFakeCCEService()
	fakeSvc.ListClustersFn = func(_ context.Context) ([]cceService.ClusterRef, error) {
		return []cceService.ClusterRef{
			// Orphan 1 — no Cluster CR anywhere, must be deleted.
			{ClusterID: "orphan-1", Name: "orphan-1", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			// Opted-out: still has its Cluster CR but skipGC annotation, must NOT delete.
			{ClusterID: "opted-out", Name: "opted-out", Tags: map[string]string{"cluster-api-provider-cce.cluster.opted-out": "owned"}},
			// Tracked: Cluster CR, no annotation, must NOT delete.
			{ClusterID: "tracked", Name: "tracked", Tags: map[string]string{"cluster-api-provider-cce.cluster.tracked": "owned"}},
		}, nil
	}
	fakeSvc.ListEipsFn = func(_ context.Context) ([]cceService.EipRef, error) {
		return []cceService.EipRef{
			{ID: "eip-orphan-1", Tags: map[string]string{"cluster-api-provider-cce.cluster.orphan-1": "owned"}},
			{ID: "eip-opted-out", Tags: map[string]string{"cluster-api-provider-cce.cluster.opted-out": "owned"}},
			{ID: "eip-tracked", Tags: map[string]string{"cluster-api-provider-cce.cluster.tracked": "owned"}},
		}, nil
	}

	gc := &GarbageCollector{
		Client: k8sClient,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
		Region: "cn-north-4",
		Interval: 1,
		ResourceTypes: []string{"eip"},
	}
	t.Setenv("CLOUD_SDK_AK", "test-ak")
	t.Setenv("CLOUD_SDK_SK", "test-sk")

	gc.sweep(ctx)

	// Only orphan-1 should be deleted (cluster + eip).
	if len(fakeSvc.DeletedClusters) != 1 {
		t.Fatalf("expected exactly 1 deleted cluster, got %d (%+v)",
			len(fakeSvc.DeletedClusters), fakeSvc.DeletedClusters)
	}
	if fakeSvc.DeletedClusters[0].ClusterID != "orphan-1" {
		t.Errorf("expected orphan-1 deleted, got %+v", fakeSvc.DeletedClusters[0])
	}
if len(fakeSvc.DeletedEips) != 1 {
		t.Fatalf("expected exactly 1 deleted EIP, got %d (%+v)",
			len(fakeSvc.DeletedEips), fakeSvc.DeletedEips)
	}
	if fakeSvc.DeletedEips[0] != "eip-orphan-1" {
		t.Errorf("expected eip-orphan-1 deleted, got %v", fakeSvc.DeletedEips[0])
	}
}
