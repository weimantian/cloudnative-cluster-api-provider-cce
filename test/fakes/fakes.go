/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package fakes provides scriptable fake implementations of the provider's
// service interfaces, used by controller tests (envtest) so no real Huawei
// Cloud credentials or API calls are required.
package fakes

import (
	"context"

	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/network"
)

// FakeCCEService is a scriptable implementation of cceService.Service.
// Defaults simulate a healthy cluster (CreateCluster -> "cluster-1",
// ShowCluster -> Available, kubeconfig -> valid content, node pool ->
// "nodepool-1", desired count as requested).
type FakeCCEService struct {
	ShowClusterFn          func(ctx context.Context, clusterID string) (*cceService.ClusterInfo, error)
	CreateClusterFn        func(ctx context.Context, in cceService.CreateClusterInput) (string, error)
	DeleteClusterFn        func(ctx context.Context, in cceService.DeleteClusterInput) error
	GetClusterKubeconfigFn func(ctx context.Context, clusterID string, durationDays int32) (string, error)
	ShowQuotasFn           func(ctx context.Context) (*cceService.QuotaInfo, error)
	CreateNodePoolFn       func(ctx context.Context, in cceService.CreateNodePoolInput) (string, error)
	ScaleNodePoolFn        func(ctx context.Context, clusterID, nodePoolID string, desiredCount int32) error
	UpdateNodePoolFn       func(ctx context.Context, in cceService.UpdateNodePoolInput) error
	DeleteNodePoolFn       func(ctx context.Context, clusterID, nodePoolID string) error
	ListNodePoolsFn        func(ctx context.Context, clusterID string) ([]cceService.NodePoolInfo, error)
	GetUpgradeInfoFn       func(ctx context.Context, clusterID string) (*cceService.UpgradeInfo, error)
	StartUpgradeFn         func(ctx context.Context, clusterID, targetVersion string) (string, error)
	ShowUpgradeTaskFn      func(ctx context.Context, clusterID, taskID string) (string, error)

	// Records for assertions.
	CreatedClusters     []cceService.CreateClusterInput
	DeletedClusters     []cceService.DeleteClusterInput
	CreatedNodePools    []cceService.CreateNodePoolInput
	ScaleCalls          []int32
	UpdateNodePoolCalls []cceService.UpdateNodePoolInput
	KubeconfigCalls     int
	StartUpgradeCalls   []string // target versions
}

// NewFakeCCEService returns a fake with healthy defaults.
func NewFakeCCEService() *FakeCCEService {
	f := &FakeCCEService{}
	f.ShowClusterFn = func(_ context.Context, clusterID string) (*cceService.ClusterInfo, error) {
		return &cceService.ClusterInfo{
			ClusterID: clusterID,
			Phase:     "Available",
			Version:   "v1.30.0",
			Endpoints: []cceService.Endpoint{{URL: "https://10.0.0.10:5443", Type: "Internal"}},
		}, nil
	}
	f.CreateClusterFn = func(_ context.Context, in cceService.CreateClusterInput) (string, error) {
		f.CreatedClusters = append(f.CreatedClusters, in)
		return "cluster-1", nil
	}
	f.DeleteClusterFn = func(_ context.Context, in cceService.DeleteClusterInput) error {
		f.DeletedClusters = append(f.DeletedClusters, in)
		return nil
	}
	f.GetClusterKubeconfigFn = func(_ context.Context, _ string, _ int32) (string, error) {
		f.KubeconfigCalls++
		return validKubeconfig, nil
	}
	f.ShowQuotasFn = func(_ context.Context) (*cceService.QuotaInfo, error) {
		return &cceService.QuotaInfo{ClusterQuotaLimit: 50, ClusterQuotaUsed: 1}, nil
	}
	f.CreateNodePoolFn = func(_ context.Context, in cceService.CreateNodePoolInput) (string, error) {
		f.CreatedNodePools = append(f.CreatedNodePools, in)
		return "nodepool-1", nil
	}
	f.ScaleNodePoolFn = func(_ context.Context, _, _ string, desiredCount int32) error {
		f.ScaleCalls = append(f.ScaleCalls, desiredCount)
		return nil
	}
	f.UpdateNodePoolFn = func(_ context.Context, in cceService.UpdateNodePoolInput) error {
		f.UpdateNodePoolCalls = append(f.UpdateNodePoolCalls, in)
		return nil
	}
	f.DeleteNodePoolFn = func(_ context.Context, _, _ string) error { return nil }
	f.ListNodePoolsFn = func(_ context.Context, _ string) ([]cceService.NodePoolInfo, error) {
		return []cceService.NodePoolInfo{{NodePoolID: "nodepool-1", Name: "pool-0", DesiredNodeCount: 3}}, nil
	}
	f.GetUpgradeInfoFn = func(_ context.Context, _ string) (*cceService.UpgradeInfo, error) {
		return &cceService.UpgradeInfo{CurrentVersion: "v1.30.0", TargetVersions: []string{"v1.31.0"}}, nil
	}
	f.StartUpgradeFn = func(_ context.Context, _, targetVersion string) (string, error) {
		f.StartUpgradeCalls = append(f.StartUpgradeCalls, targetVersion)
		return "upgrade-task-1", nil
	}
	f.ShowUpgradeTaskFn = func(_ context.Context, _, _ string) (string, error) {
		return cceService.UpgradeTaskPhaseSuccess, nil
	}
	return f
}

// FakeNetworkValidator is a scriptable network.ValidatorInterface.
type FakeNetworkValidator struct {
	Issues []network.Issue
}

// NewFakeNetworkValidator returns a validator that reports no issues.
func NewFakeNetworkValidator() *FakeNetworkValidator {
	return &FakeNetworkValidator{}
}

// Validate implements network.ValidatorInterface.
func (f *FakeNetworkValidator) Validate(_ context.Context, _ network.ValidateInput) ([]network.Issue, error) {
	return f.Issues, nil
}

// --- Service interface methods ---

// ShowCluster implements cceService.Service.
func (f *FakeCCEService) ShowCluster(ctx context.Context, clusterID string) (*cceService.ClusterInfo, error) {
	return f.ShowClusterFn(ctx, clusterID)
}

// CreateCluster implements cceService.Service.
func (f *FakeCCEService) CreateCluster(ctx context.Context, in cceService.CreateClusterInput) (string, error) {
	return f.CreateClusterFn(ctx, in)
}

// DeleteCluster implements cceService.Service.
func (f *FakeCCEService) DeleteCluster(ctx context.Context, in cceService.DeleteClusterInput) error {
	return f.DeleteClusterFn(ctx, in)
}

// GetClusterKubeconfig implements cceService.Service.
func (f *FakeCCEService) GetClusterKubeconfig(ctx context.Context, clusterID string, durationDays int32) (string, error) {
	return f.GetClusterKubeconfigFn(ctx, clusterID, durationDays)
}

// ShowQuotas implements cceService.Service.
func (f *FakeCCEService) ShowQuotas(ctx context.Context) (*cceService.QuotaInfo, error) {
	return f.ShowQuotasFn(ctx)
}

// CreateNodePool implements cceService.Service.
func (f *FakeCCEService) CreateNodePool(ctx context.Context, in cceService.CreateNodePoolInput) (string, error) {
	return f.CreateNodePoolFn(ctx, in)
}

// ScaleNodePool implements cceService.Service.
func (f *FakeCCEService) ScaleNodePool(ctx context.Context, clusterID, nodePoolID string, desiredCount int32) error {
	return f.ScaleNodePoolFn(ctx, clusterID, nodePoolID, desiredCount)
}

// UpdateNodePool implements cceService.Service.
func (f *FakeCCEService) UpdateNodePool(ctx context.Context, in cceService.UpdateNodePoolInput) error {
	return f.UpdateNodePoolFn(ctx, in)
}

// DeleteNodePool implements cceService.Service.
func (f *FakeCCEService) DeleteNodePool(ctx context.Context, clusterID, nodePoolID string) error {
	return f.DeleteNodePoolFn(ctx, clusterID, nodePoolID)
}

// ListNodePools implements cceService.Service.
func (f *FakeCCEService) ListNodePools(ctx context.Context, clusterID string) ([]cceService.NodePoolInfo, error) {
	return f.ListNodePoolsFn(ctx, clusterID)
}

// GetUpgradeInfo implements cceService.Service.
func (f *FakeCCEService) GetUpgradeInfo(ctx context.Context, clusterID string) (*cceService.UpgradeInfo, error) {
	return f.GetUpgradeInfoFn(ctx, clusterID)
}

// StartUpgrade implements cceService.Service.
func (f *FakeCCEService) StartUpgrade(ctx context.Context, clusterID, targetVersion string) (string, error) {
	return f.StartUpgradeFn(ctx, clusterID, targetVersion)
}

// ShowUpgradeTask implements cceService.Service.
func (f *FakeCCEService) ShowUpgradeTask(ctx context.Context, clusterID, taskID string) (string, error) {
	return f.ShowUpgradeTaskFn(ctx, clusterID, taskID)
}

// validKubeconfig is a minimal parseable kubeconfig for the Secret.
const validKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: internalCluster
  cluster:
    server: https://10.0.0.10:5443
users:
- name: internal
  user: {}
contexts:
- name: internal
  context:
    cluster: internalCluster
    user: internal
current-context: internal
`
