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
	"fmt"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	iamService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/iam"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/network"
)

// FakeCCEService is a scriptable implementation of cceService.Service.
// Defaults simulate a healthy cluster (CreateCluster -> "cluster-1",
// ShowCluster -> Available, kubeconfig -> valid content, node pool ->
// "nodepool-1", desired count as requested).
type FakeCCEService struct {
	ShowClusterFn            func(ctx context.Context, clusterID string) (*cceService.ClusterInfo, error)
	CreateClusterFn          func(ctx context.Context, in cceService.CreateClusterInput) (string, error)
	DeleteClusterFn          func(ctx context.Context, in cceService.DeleteClusterInput) error
	GetClusterKubeconfigFn   func(ctx context.Context, clusterID string, durationDays int32) (string, error)
	ShowQuotasFn             func(ctx context.Context) (*cceService.QuotaInfo, error)
	ListClustersFn           func(ctx context.Context) ([]cceService.ClusterRef, error)
	CreateNodePoolFn         func(ctx context.Context, in cceService.CreateNodePoolInput) (string, error)
	ScaleNodePoolFn          func(ctx context.Context, clusterID, nodePoolID string, desiredCount int32) error
	UpdateNodePoolFn         func(ctx context.Context, in cceService.UpdateNodePoolInput) error
	DeleteNodePoolFn         func(ctx context.Context, clusterID, nodePoolID string) error
	ListNodePoolsFn          func(ctx context.Context, clusterID string) ([]cceService.NodePoolInfo, error)
	ListNodesFn              func(ctx context.Context, clusterID, nodePoolID string) ([]string, error)
	ListNodesWithStatusFn    func(ctx context.Context, clusterID string) ([]cceService.NodeInfo, error)
	ResetNodeFn              func(ctx context.Context, clusterID string, nodeIDs []string) error
	GetUpgradeInfoFn         func(ctx context.Context, clusterID string) (*cceService.UpgradeInfo, error)
	StartUpgradeFn           func(ctx context.Context, clusterID, targetVersion string) (string, error)
	ShowUpgradeTaskFn        func(ctx context.Context, clusterID, taskID string) (string, error)
	CreateAddonInstanceFn    func(ctx context.Context, in cceService.AddonInput) (string, error)
	UpdateAddonInstanceFn    func(ctx context.Context, in cceService.AddonInput) error
	ListAddonInstancesFn     func(ctx context.Context, clusterID string) ([]cceService.AddonInfo, error)
	DeleteAddonInstanceFn    func(ctx context.Context, clusterID, addonID string) error
	CreatePodIdentityFn      func(ctx context.Context, in cceService.PodIdentityAssociationInput) (string, error)
	ListPodIdentityFn        func(ctx context.Context, clusterID string) ([]cceService.PodIdentityAssociationInfo, error)
	DeletePodIdentityFn      func(ctx context.Context, clusterID, associationID string) error
	UpgradeNodePoolFn        func(ctx context.Context, clusterID, nodePoolID string, maxUnavailable int32) error
	ShowClusterLogConfigFn   func(ctx context.Context, clusterID string) (*cceService.LogConfigInfo, error)
	UpdateClusterLogConfigFn func(ctx context.Context, clusterID string, ttlInDays int32, logs []cceService.LogConfigInput) error
	CreateAccessPolicyFn     func(ctx context.Context, in cceService.AccessPolicyInput) (string, error)
	UpdateAccessPolicyFn     func(ctx context.Context, policyID string, in cceService.AccessPolicyInput) error
	ListAccessPoliciesFn     func(ctx context.Context) ([]cceService.AccessPolicyInfo, error)
	DeleteAccessPolicyFn     func(ctx context.Context, policyID string) error
	ListEipsFn               func(ctx context.Context) ([]cceService.EipRef, error)
	DeleteEipFn              func(ctx context.Context, eipID string) error
	ListVolumesFn            func(ctx context.Context) ([]cceService.VolumeRef, error)
	DeleteVolumeFn           func(ctx context.Context, volumeID string) error
	ListVpcsFn               func(ctx context.Context) ([]cceService.VpcRef, error)
	DeleteVpcFn              func(ctx context.Context, vpcID string) error
	ListNatGatewaysFn        func(ctx context.Context) ([]cceService.NatGatewayRef, error)
	DeleteNatGatewayFn       func(ctx context.Context, gatewayID string) error
	// DeletedEips / DeletedVolumes record GC deletions for assertions.
	DeletedEips    []string
	DeletedVolumes []string
	// DeletedVpcs / DeletedNatGateways record GC deletions for assertions.
	DeletedVpcs        []string
	DeletedNatGateways []string
	// ResetNodeCalls records node-repair resets for assertions.
	ResetNodeCalls [][]string

	// Records for assertions.
	CreatedClusters      []cceService.CreateClusterInput
	DeletedClusters      []cceService.DeleteClusterInput
	CreatedNodePools     []cceService.CreateNodePoolInput
	ScaleCalls           []int32
	UpdateNodePoolCalls  []cceService.UpdateNodePoolInput
	KubeconfigCalls      int
	StartUpgradeCalls    []string // target versions
	AddonCreateCalls     []cceService.AddonInput
	AddonUpdateCalls     []cceService.AddonInput
	AddonDeleteCalls     []string               // addon IDs
	Addons               []cceService.AddonInfo // returned by ListAddonInstances
	PodIdentityCreate    []cceService.PodIdentityAssociationInput
	AccessPolicyCreate   []cceService.AccessPolicyInput
	AccessPolicyUpdate   []cceService.AccessPolicyInput
	AccessPolicyDelete   []string                      // policy IDs
	AccessPolicies       []cceService.AccessPolicyInfo // returned by ListAccessPolicies
	PodIdentityDelete    []string                      // association IDs
	PodIdentities        []cceService.PodIdentityAssociationInfo
	UpgradeNodePoolCalls []struct {
		ClusterID      string
		NodePoolID     string
		MaxUnavailable int32
	}
	LogConfigCalls []struct {
		ClusterID string
		TTLInDays int32
		Logs      []cceService.LogConfigInput
	}
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
	f.ListClustersFn = func(_ context.Context) ([]cceService.ClusterRef, error) {
		return []cceService.ClusterRef{}, nil
	}
	f.ListEipsFn = func(_ context.Context) ([]cceService.EipRef, error) {
		return []cceService.EipRef{}, nil
	}
	f.DeleteEipFn = func(_ context.Context, eipID string) error {
		f.DeletedEips = append(f.DeletedEips, eipID)
		return nil
	}
	f.ListVolumesFn = func(_ context.Context) ([]cceService.VolumeRef, error) {
		return []cceService.VolumeRef{}, nil
	}
	f.DeleteVolumeFn = func(_ context.Context, volumeID string) error {
		f.DeletedVolumes = append(f.DeletedVolumes, volumeID)
		return nil
	}
	f.ListVpcsFn = func(_ context.Context) ([]cceService.VpcRef, error) {
		return []cceService.VpcRef{}, nil
	}
	f.DeleteVpcFn = func(_ context.Context, vpcID string) error {
		f.DeletedVpcs = append(f.DeletedVpcs, vpcID)
		return nil
	}
	f.ListNatGatewaysFn = func(_ context.Context) ([]cceService.NatGatewayRef, error) {
		return []cceService.NatGatewayRef{}, nil
	}
	f.DeleteNatGatewayFn = func(_ context.Context, gatewayID string) error {
		f.DeletedNatGateways = append(f.DeletedNatGateways, gatewayID)
		return nil
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
	f.UpgradeNodePoolFn = func(_ context.Context, _, _ string, _ int32) error { return nil }
	f.ShowClusterLogConfigFn = func(_ context.Context, _ string) (*cceService.LogConfigInfo, error) {
		return &cceService.LogConfigInfo{}, nil
	}
	f.UpdateClusterLogConfigFn = func(_ context.Context, _ string, _ int32, _ []cceService.LogConfigInput) error {
		return nil
	}
	f.CreateAccessPolicyFn = func(_ context.Context, in cceService.AccessPolicyInput) (string, error) {
		f.AccessPolicyCreate = append(f.AccessPolicyCreate, in)
		return "access-policy-" + in.Name, nil
	}
	f.UpdateAccessPolicyFn = func(_ context.Context, _ string, in cceService.AccessPolicyInput) error {
		f.AccessPolicyUpdate = append(f.AccessPolicyUpdate, in)
		return nil
	}
	f.ListAccessPoliciesFn = func(_ context.Context) ([]cceService.AccessPolicyInfo, error) {
		return f.AccessPolicies, nil
	}
	f.DeleteAccessPolicyFn = func(_ context.Context, policyID string) error {
		f.AccessPolicyDelete = append(f.AccessPolicyDelete, policyID)
		return nil
	}
	f.DeleteNodePoolFn = func(_ context.Context, _, _ string) error { return nil }
	f.ListNodePoolsFn = func(_ context.Context, _ string) ([]cceService.NodePoolInfo, error) {
		return []cceService.NodePoolInfo{{NodePoolID: "nodepool-1", Name: "pool-0", DesiredNodeCount: 3, NodeCount: 3, ActiveNodeCount: 3}}, nil
	}
	f.ListNodesFn = func(_ context.Context, _, _ string) ([]string, error) {
		return nil, nil
	}
	f.ListNodesWithStatusFn = func(_ context.Context, _ string) ([]cceService.NodeInfo, error) {
		return nil, nil
	}
	f.ResetNodeFn = func(_ context.Context, _ string, nodeIDs []string) error {
		f.ResetNodeCalls = append(f.ResetNodeCalls, nodeIDs)
		return nil
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
	f.CreateAddonInstanceFn = func(_ context.Context, in cceService.AddonInput) (string, error) {
		f.AddonCreateCalls = append(f.AddonCreateCalls, in)
		return "addon-id-" + in.Name, nil
	}
	f.UpdateAddonInstanceFn = func(_ context.Context, in cceService.AddonInput) error {
		f.AddonUpdateCalls = append(f.AddonUpdateCalls, in)
		return nil
	}
	f.ListAddonInstancesFn = func(_ context.Context, _ string) ([]cceService.AddonInfo, error) {
		return f.Addons, nil
	}
	f.DeleteAddonInstanceFn = func(_ context.Context, _, addonID string) error {
		f.AddonDeleteCalls = append(f.AddonDeleteCalls, addonID)
		return nil
	}
	f.CreatePodIdentityFn = func(_ context.Context, in cceService.PodIdentityAssociationInput) (string, error) {
		f.PodIdentityCreate = append(f.PodIdentityCreate, in)
		return "podid-" + in.Namespace + "-" + in.ServiceAccount, nil
	}
	f.ListPodIdentityFn = func(_ context.Context, _ string) ([]cceService.PodIdentityAssociationInfo, error) {
		return f.PodIdentities, nil
	}
	f.DeletePodIdentityFn = func(_ context.Context, _, associationID string) error {
		f.PodIdentityDelete = append(f.PodIdentityDelete, associationID)
		return nil
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

// FakeIAMService is a scriptable iamService.Service for the trust-agency
// auto-creation path. Defaults to a no-op (agency exists / created).
type FakeIAMService struct {
	EnsureAgencyFn func(ctx context.Context, agencyName, trustPolicy string) error
	// EnsuredAgencies records EnsureAgency calls for assertions.
	EnsuredAgencies []struct {
		AgencyName  string
		TrustPolicy string
	}
}

// NewFakeIAMService returns a fake that records calls and succeeds.
func NewFakeIAMService() *FakeIAMService {
	return &FakeIAMService{}
}

var _ iamService.Service = (*FakeIAMService)(nil)

// EnsureAgency implements iamService.Service.
func (f *FakeIAMService) EnsureAgency(ctx context.Context, agencyName, trustPolicy string) error {
	f.EnsuredAgencies = append(f.EnsuredAgencies, struct {
		AgencyName  string
		TrustPolicy string
	}{agencyName, trustPolicy})
	if f.EnsureAgencyFn != nil {
		return f.EnsureAgencyFn(ctx, agencyName, trustPolicy)
	}
	return nil
}

// FakeCredentialProvider is a scriptable credentials.Provider that returns
// canned temporary security credentials without contacting Huawei Cloud STS.
type FakeCredentialProvider struct {
	AssumeAgencyFn func(ctx context.Context, region, agencyName, accessKey, secretKey string) (*credentials.Credentials, error)
}

// NewFakeCredentialProvider returns a provider that echoes the input AK/SK and
// sets a fake security token.
func NewFakeCredentialProvider() *FakeCredentialProvider {
	return &FakeCredentialProvider{
		AssumeAgencyFn: func(_ context.Context, _, _, accessKey, secretKey string) (*credentials.Credentials, error) {
			return &credentials.Credentials{AccessKey: accessKey, SecretKey: secretKey, SecurityToken: "fake-security-token"}, nil
		},
	}
}

// AssumeAgency implements credentials.Provider.
func (f *FakeCredentialProvider) AssumeAgency(ctx context.Context, region, agencyName, accessKey, secretKey string) (*credentials.Credentials, error) {
	return f.AssumeAgencyFn(ctx, region, agencyName, accessKey, secretKey)
}

// FakeNetworkManager is a scriptable network.ManagerInterface for the
// managed-network (VPC/subnets/NAT) reconciler paths.
type FakeNetworkManager struct {
	// ReconcileVpcFn / ReconcileSubnetsFn / ReconcileNatGatewayFn override the
	// corresponding step; when nil the step backfills deterministic ResourceIDs.
	ReconcileVpcFn        func(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	ReconcileSubnetsFn    func(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	ReconcileNatGatewayFn func(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	// DeleteFn is called by DeleteNetwork; when nil, deletion succeeds.
	DeleteFn func(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	// ReconcileCalls counts the total step invocations (Vpc+Subnets+NatGateway).
	ReconcileCalls int
	DeleteCalls    int
}

func (f *FakeNetworkManager) ReconcileVpc(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	f.ReconcileCalls++
	if f.ReconcileVpcFn != nil {
		return f.ReconcileVpcFn(ctx, spec, clusterName)
	}
	if spec.VPC.ID == "" && spec.VPC.ResourceID == "" {
		spec.VPC.ResourceID = "vpc-managed-fake"
	}
	return nil
}

func (f *FakeNetworkManager) ReconcileSubnets(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	f.ReconcileCalls++
	if f.ReconcileSubnetsFn != nil {
		return f.ReconcileSubnetsFn(ctx, spec, clusterName)
	}
	for i := range spec.Subnets {
		if spec.Subnets[i].ID == "" && spec.Subnets[i].ResourceID == "" {
			spec.Subnets[i].ResourceID = fmt.Sprintf("subnet-managed-fake-%d", i)
		}
	}
	return nil
}

func (f *FakeNetworkManager) ReconcileNatGateway(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	f.ReconcileCalls++
	if f.ReconcileNatGatewayFn != nil {
		return f.ReconcileNatGatewayFn(ctx, spec, clusterName)
	}
	if spec.NatGateway != nil && spec.NatGateway.ResourceID == "" {
		spec.NatGateway.ResourceID = "nat-fake"
		spec.NatGateway.EIPResourceID = "eip-fake"
	}
	return nil
}

func (f *FakeNetworkManager) DeleteNetwork(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	f.DeleteCalls++
	if f.DeleteFn != nil {
		return f.DeleteFn(ctx, spec, clusterName)
	}
	return nil
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

// ListClusters implements cceService.Service.
func (f *FakeCCEService) ListClusters(ctx context.Context) ([]cceService.ClusterRef, error) {
	return f.ListClustersFn(ctx)
}

// ListEips implements cceService.Service.
func (f *FakeCCEService) ListEips(ctx context.Context) ([]cceService.EipRef, error) {
	return f.ListEipsFn(ctx)
}

// DeleteEip implements cceService.Service.
func (f *FakeCCEService) DeleteEip(ctx context.Context, eipID string) error {
	return f.DeleteEipFn(ctx, eipID)
}

// ListVolumes implements cceService.Service.
func (f *FakeCCEService) ListVolumes(ctx context.Context) ([]cceService.VolumeRef, error) {
	return f.ListVolumesFn(ctx)
}

// DeleteVolume implements cceService.Service.
func (f *FakeCCEService) DeleteVolume(ctx context.Context, volumeID string) error {
	return f.DeleteVolumeFn(ctx, volumeID)
}

// ListVpcs implements cceService.Service.
func (f *FakeCCEService) ListVpcs(ctx context.Context) ([]cceService.VpcRef, error) {
	return f.ListVpcsFn(ctx)
}

// DeleteVpc implements cceService.Service.
func (f *FakeCCEService) DeleteVpc(ctx context.Context, vpcID string) error {
	return f.DeleteVpcFn(ctx, vpcID)
}

// ListNatGateways implements cceService.Service.
func (f *FakeCCEService) ListNatGateways(ctx context.Context) ([]cceService.NatGatewayRef, error) {
	return f.ListNatGatewaysFn(ctx)
}

// DeleteNatGateway implements cceService.Service.
func (f *FakeCCEService) DeleteNatGateway(ctx context.Context, gatewayID string) error {
	return f.DeleteNatGatewayFn(ctx, gatewayID)
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

// ListNodes implements cceService.Service.
func (f *FakeCCEService) ListNodes(ctx context.Context, clusterID, nodePoolID string) ([]string, error) {
	return f.ListNodesFn(ctx, clusterID, nodePoolID)
}

// ListNodesWithStatus implements cceService.Service.
func (f *FakeCCEService) ListNodesWithStatus(ctx context.Context, clusterID string) ([]cceService.NodeInfo, error) {
	return f.ListNodesWithStatusFn(ctx, clusterID)
}

// ResetNode implements cceService.Service.
func (f *FakeCCEService) ResetNode(ctx context.Context, clusterID string, nodeIDs []string) error {
	return f.ResetNodeFn(ctx, clusterID, nodeIDs)
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

// CreateAddonInstance implements cceService.Service.
func (f *FakeCCEService) CreateAddonInstance(ctx context.Context, in cceService.AddonInput) (string, error) {
	return f.CreateAddonInstanceFn(ctx, in)
}

// UpdateAddonInstance implements cceService.Service.
func (f *FakeCCEService) UpdateAddonInstance(ctx context.Context, in cceService.AddonInput) error {
	return f.UpdateAddonInstanceFn(ctx, in)
}

// ListAddonInstances implements cceService.Service.
func (f *FakeCCEService) ListAddonInstances(ctx context.Context, clusterID string) ([]cceService.AddonInfo, error) {
	return f.ListAddonInstancesFn(ctx, clusterID)
}

// DeleteAddonInstance implements cceService.Service.
func (f *FakeCCEService) DeleteAddonInstance(ctx context.Context, clusterID, addonID string) error {
	return f.DeleteAddonInstanceFn(ctx, clusterID, addonID)
}

// CreatePodIdentityAssociation implements cceService.Service.
func (f *FakeCCEService) CreatePodIdentityAssociation(ctx context.Context, in cceService.PodIdentityAssociationInput) (string, error) {
	return f.CreatePodIdentityFn(ctx, in)
}

// ListPodIdentityAssociations implements cceService.Service.
func (f *FakeCCEService) ListPodIdentityAssociations(ctx context.Context, clusterID string) ([]cceService.PodIdentityAssociationInfo, error) {
	return f.ListPodIdentityFn(ctx, clusterID)
}

// DeletePodIdentityAssociation implements cceService.Service.
func (f *FakeCCEService) DeletePodIdentityAssociation(ctx context.Context, clusterID, associationID string) error {
	return f.DeletePodIdentityFn(ctx, clusterID, associationID)
}

// CreateAccessPolicy implements cceService.Service.
func (f *FakeCCEService) CreateAccessPolicy(ctx context.Context, in cceService.AccessPolicyInput) (string, error) {
	return f.CreateAccessPolicyFn(ctx, in)
}

// UpdateAccessPolicy implements cceService.Service.
func (f *FakeCCEService) UpdateAccessPolicy(ctx context.Context, policyID string, in cceService.AccessPolicyInput) error {
	return f.UpdateAccessPolicyFn(ctx, policyID, in)
}

// ListAccessPolicies implements cceService.Service.
func (f *FakeCCEService) ListAccessPolicies(ctx context.Context) ([]cceService.AccessPolicyInfo, error) {
	return f.ListAccessPoliciesFn(ctx)
}

// DeleteAccessPolicy implements cceService.Service.
func (f *FakeCCEService) DeleteAccessPolicy(ctx context.Context, policyID string) error {
	return f.DeleteAccessPolicyFn(ctx, policyID)
}

// UpgradeNodePool implements cceService.Service.
func (f *FakeCCEService) UpgradeNodePool(ctx context.Context, clusterID, nodePoolID string, maxUnavailable int32) error {
	f.UpgradeNodePoolCalls = append(f.UpgradeNodePoolCalls, struct {
		ClusterID      string
		NodePoolID     string
		MaxUnavailable int32
	}{clusterID, nodePoolID, maxUnavailable})
	return f.UpgradeNodePoolFn(ctx, clusterID, nodePoolID, maxUnavailable)
}

// ShowClusterLogConfig implements cceService.Service.
func (f *FakeCCEService) ShowClusterLogConfig(ctx context.Context, clusterID string) (*cceService.LogConfigInfo, error) {
	return f.ShowClusterLogConfigFn(ctx, clusterID)
}

// UpdateClusterLogConfig implements cceService.Service.
func (f *FakeCCEService) UpdateClusterLogConfig(ctx context.Context, clusterID string, ttlInDays int32, logs []cceService.LogConfigInput) error {
	f.LogConfigCalls = append(f.LogConfigCalls, struct {
		ClusterID string
		TTLInDays int32
		Logs      []cceService.LogConfigInput
	}{clusterID, ttlInDays, logs})
	return f.UpdateClusterLogConfigFn(ctx, clusterID, ttlInDays, logs)
}
