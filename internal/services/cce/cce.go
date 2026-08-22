/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package cce

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
	evsv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/evs/v2"
	evsmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/evs/v2/model"
	evsregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/evs/v2/region"
	natv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2"
	natmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/model"
	natregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// Client is the default CCE SDK-backed implementation of Service.
type Client struct {
	cce *ccev3.CceClient
	eip *eipv2.EipClient
	evs *evsv2.EvsClient
	vpc *vpcv2.VpcClient
	nat *natv2.NatClient
}

// clientCache caches built CCE SDK clients by (region, ak, sk). The SDK
// client is stateless (it only holds an http.Client + endpoint config), so
// it is safe to share across concurrent reconciles. Credentials are stable
// per cluster; rotating them requires a controller restart to drop the cache
// (CAPA rebuilds a session per scope, i.e. per reconcile — this cache avoids
// even that per-reconcile HTTP-client construction).
var clientCache sync.Map

// NewClient builds (or returns a cached) CCE client from AK/SK and region.
// Pattern follows CAPHW pkg/scope/clients.go (SafeValueOf + Builder pattern).
func NewClient(regionID, ak, sk string) (*Client, error) {
	key := regionID + "\x00" + ak + "\x00" + sk
	if v, ok := clientCache.Load(key); ok {
		return v.(*Client), nil
	}
	region, err := cceRegion.SafeValueOf(regionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve CCE region %q", regionID)
	}
	cred, err := basic.NewCredentialsBuilder().
		WithAk(ak).
		WithSk(sk).
		SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build CCE credentials")
	}
	hcClient, err := ccev3.CceClientBuilder().
		WithRegion(region).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).
		SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build CCE HTTP client")
	}
	c := &Client{cce: ccev3.NewCceClient(hcClient)}
	if err := buildAuxClients(c, regionID, cred); err != nil {
		return nil, err
	}
	clientCache.Store(key, c)
	return c, nil
}

// buildAuxClients attaches the EIP and EVS clients to c (used by the GC
// orphan sweeper). A build failure is fatal: the client cache would
// otherwise store a partially-initialized client.
func buildAuxClients(c *Client, regionID string, cred auth.ICredential) error {
	eipRegion, err := eipregion.SafeValueOf(regionID)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve EIP region %q", regionID)
	}
	eipHC, err := eipv2.EipClientBuilder().WithRegion(eipRegion).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return errors.Wrap(err, "failed to build EIP client")
	}
	c.eip = eipv2.NewEipClient(eipHC)
	evsRegion, err := evsregion.SafeValueOf(regionID)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve EVS region %q", regionID)
	}
	evsHC, err := evsv2.EvsClientBuilder().WithRegion(evsRegion).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return errors.Wrap(err, "failed to build EVS client")
	}
	c.evs = evsv2.NewEvsClient(evsHC)
	vpcRegion, err := vpcregion.SafeValueOf(regionID)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve VPC region %q", regionID)
	}
	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(vpcRegion).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return errors.Wrap(err, "failed to build VPC client")
	}
	c.vpc = vpcv2.NewVpcClient(vpcHC)
	natRegion, err := natregion.SafeValueOf(regionID)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve NAT region %q", regionID)
	}
	natHC, err := natv2.NatClientBuilder().WithRegion(natRegion).WithCredential(cred).WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	if err != nil {
		return errors.Wrap(err, "failed to build NAT client")
	}
	c.nat = natv2.NewNatClient(natHC)
	return nil
}

// ShowCluster implements Service.
func (s *Client) ShowCluster(_ context.Context, clusterID string) (*ClusterInfo, error) {
	resp, err := s.cce.ShowCluster(&model.ShowClusterRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrapf(err, "ShowCluster %s failed", clusterID)
	}
	info := &ClusterInfo{}
	if resp.Metadata != nil && resp.Metadata.Uid != nil {
		info.ClusterID = *resp.Metadata.Uid
	}
	if resp.Status != nil {
		if resp.Status.Phase != nil {
			info.Phase = *resp.Status.Phase
		}
		if resp.Status.Endpoints != nil {
			for _, ep := range *resp.Status.Endpoints {
				e := Endpoint{}
				if ep.Url != nil {
					e.URL = *ep.Url
				}
				if ep.Type != nil {
					e.Type = *ep.Type
				}
				info.Endpoints = append(info.Endpoints, e)
			}
		}
	}
	if resp.Spec != nil && resp.Spec.Version != nil {
		info.Version = *resp.Spec.Version
	}
	return info, nil
}

// CreateCluster implements Service.
func (s *Client) CreateCluster(ctx context.Context, in CreateClusterInput) (string, error) {
	// hostNetwork (VPC + node subnet) is REQUIRED by the official API
	// (CreateCluster.txt: "VPC是集群内节点之间的通信依赖,所以是必选的参数集").
	// Fail fast here instead of sending a request the platform will reject.
	if in.HostNetworkVpcID == "" || in.HostNetworkSubnetID == "" {
		return "", errors.New("CreateCluster: hostNetwork vpc and subnet are required")
	}
	spec := &model.ClusterSpec{
		// category: empty is derived from the network mode per official docs
		// ("容器网络参数设置为eni模式时,默认为Turbo;否则默认为CCE").
		Category:    clusterCategory(in.Category, in.ContainerNetworkMode),
		BillingMode: int32Ptr(in.BillingMode),
		ContainerNetwork: &model.ContainerNetwork{
			Mode: model.GetContainerNetworkModeEnum().OVERLAY_L2, // replaced below
		},
	}
	if in.AgencyName != "" {
		spec.AgencyName = stringPtr(in.AgencyName)
	}
	// flavor/version: official defaults apply only when UNCONFIGURED — an
	// explicit empty string would be rejected, so omit the fields when empty.
	if in.Flavor != "" {
		spec.Flavor = stringPtr(in.Flavor)
	}
	if in.Version != "" {
		spec.Version = stringPtr(in.Version)
	}
	// containerNetwork mode mapping (official enum: overlay_l2/vpc-router/eni).
	switch in.ContainerNetworkMode {
	case "vpc-router":
		spec.ContainerNetwork.Mode = model.GetContainerNetworkModeEnum().VPC_ROUTER
	case "eni":
		spec.ContainerNetwork.Mode = model.GetContainerNetworkModeEnum().ENI
	default:
		spec.ContainerNetwork.Mode = model.GetContainerNetworkModeEnum().OVERLAY_L2
	}
	if in.ContainerNetworkCIDR != "" {
		spec.ContainerNetwork.Cidr = stringPtr(in.ContainerNetworkCIDR)
	}
	if len(in.ContainerNetworkCIDRs) > 0 {
		cidrs := make([]model.ContainerCidr, 0, len(in.ContainerNetworkCIDRs))
		for _, c := range in.ContainerNetworkCIDRs {
			cidrs = append(cidrs, model.ContainerCidr{Cidr: c})
		}
		spec.ContainerNetwork.Cidrs = &cidrs
	}
	if in.ServiceCIDR != "" || in.ServiceIPv6CIDR != "" {
		sn := &model.ServiceNetwork{}
		if in.ServiceCIDR != "" {
			sn.IPv4CIDR = stringPtr(in.ServiceCIDR)
		}
		if in.ServiceIPv6CIDR != "" {
			sn.IPv6CIDR = stringPtr(in.ServiceIPv6CIDR)
		}
		spec.ServiceNetwork = sn
	}
	if in.Ipv6Enable != nil {
		spec.Ipv6enable = in.Ipv6Enable
	}
	if in.EnableAutopilot != nil {
		spec.EnableAutopilot = in.EnableAutopilot
	}
	if len(in.CustomSAN) > 0 {
		spec.CustomSan = &in.CustomSAN
	}
	if len(in.ENISubnets) > 0 {
		subnets := make([]model.NetworkSubnet, 0, len(in.ENISubnets))
		for _, id := range in.ENISubnets {
			subnets = append(subnets, model.NetworkSubnet{SubnetID: id})
		}
		spec.EniNetwork = &model.EniNetwork{Subnets: &subnets}
	}
	if in.PublicAccess {
		// Empty whitelist defaults to 0.0.0.0/0 (official PublicAccess model);
		// explicit cidrs restrict the public API to the given networks.
		pa := &model.PublicAccess{}
		if len(in.PublicAccessCIDRs) > 0 {
			pa.Cidrs = &in.PublicAccessCIDRs
		}
		spec.PublicAccess = pa
	}
	spec.HostNetwork = &model.HostNetwork{Vpc: in.HostNetworkVpcID, Subnet: in.HostNetworkSubnetID}
	// Ownership + user tags -> CCE clusterTags (official ResourceTag array).
	spec.ClusterTags = toClusterTags(in.Name, in.Tags)
	if in.EncryptionConfig != nil && in.EncryptionConfig.Mode != "" {
		spec.EncryptionConfig = &model.EncryptionConfig{
			Mode: encryptionModeEnum(in.EncryptionConfig.Mode),
		}
	}
	if in.Authentication != nil && in.Authentication.Mode != "" {
		auth := &model.Authentication{Mode: stringPtr(in.Authentication.Mode)}
		if in.Authentication.AuthenticatingProxy != nil {
			p := in.Authentication.AuthenticatingProxy
			auth.AuthenticatingProxy = &model.AuthenticatingProxy{
				Ca:         base64StrPtr(p.CA),
				Cert:       base64StrPtr(p.Cert),
				PrivateKey: base64StrPtr(p.PrivateKey),
			}
		}
		spec.Authentication = auth
	}
	if in.BillingMode == 1 {
		// Subscription clusters require periodType/periodNum (official
		// ClusterExtendParam: "billingMode为1(包周期)时生效,且为必选").
		if in.PeriodType == "" {
			return "", errors.New("CreateCluster: periodType is required when billingMode=1 (subscription)")
		}
		spec.ExtendParam = &model.ClusterExtendParam{
			PeriodType:  &in.PeriodType,
			PeriodNum:   int32Ptr(in.PeriodNum),
			IsAutoRenew: stringPtr(in.IsAutoRenew),
			IsAutoPay:   stringPtr(in.IsAutoPay),
		}
	}

	cluster := &model.Cluster{
		Kind:       "Cluster",
		ApiVersion: "v3",
		Metadata:   &model.ClusterMetadata{Name: in.Name},
		Spec:       spec,
	}
	resp, err := s.cce.CreateCluster(&model.CreateClusterRequest{Body: cluster})
	if err != nil {
		// Idempotent create: if the cluster already exists — e.g. a previous
		// create succeeded but the response was lost to throttling (verified
		// live: APIGW.0308 limit 10/min on writes) — adopt it by name instead
		// of failing on a container-CIDR/exists conflict. If no same-name
		// cluster is found, the conflict is a genuine configuration error and
		// the original error is returned.
		if clouderrors.IsConflict(err) {
			if id, ferr := s.findClusterIDByName(ctx, in.Name); ferr == nil && id != "" {
				// Ownership guard: only adopt a cluster that is actually
				// provisioned (not Deleting/Unavailable) — adopting a dying or
				// foreign same-name cluster would lead to wrong operations.
				if info, serr := s.ShowCluster(ctx, id); serr == nil && (info.Phase == "Available" || info.Phase == "Creating") {
					return id, nil
				}
				return "", errors.Wrapf(err, "CreateCluster conflicted but existing cluster %q is not adoptable", in.Name)
			}
		}
		return "", errors.Wrap(err, "CreateCluster failed")
	}
	if resp.Metadata != nil && resp.Metadata.Uid != nil {
		return *resp.Metadata.Uid, nil
	}
	// Subscription (billingMode=1) cluster creates do NOT return a cluster ID
	// (official model_cluster_metadata.go note) — fall back to lookup by name.
	// The returned ID is the handle controllers poll with ShowCluster until the
	// cluster is Available (verified live against real CCE — questionnaire Q1).
	id, err := s.findClusterIDByName(ctx, in.Name)
	if err != nil {
		return "", errors.Wrap(err, "CreateCluster returned no ID and lookup by name failed")
	}
	return id, nil
}

// findClusterIDByName looks up a cluster by its metadata name.
func (s *Client) findClusterIDByName(ctx context.Context, name string) (string, error) {
	resp, err := s.cce.ListClusters(&model.ListClustersRequest{})
	if err != nil {
		return "", errors.Wrap(err, "ListClusters failed")
	}
	if resp.Items != nil {
		for _, c := range *resp.Items {
			if c.Metadata != nil && c.Metadata.Name == name && c.Metadata.Uid != nil {
				return *c.Metadata.Uid, nil
			}
		}
	}
	return "", errors.Errorf("cluster %q not found by name", name)
}

// DeleteCluster implements Service. Delete options default to "delete
// everything the provider manages" to avoid leftovers (official defaults leave
// EVS/storage behind — verified cce_02_0241, questionnaire Q8).
func (s *Client) DeleteCluster(_ context.Context, in DeleteClusterInput) error {
	req := &model.DeleteClusterRequest{
		ClusterId: in.ClusterID,
	}
	if in.DeleteEVS {
		v := model.GetDeleteClusterRequestDeleteEvsEnum().BLOCK
		req.DeleteEvs = &v
	}
	if in.DeleteENI {
		v := model.GetDeleteClusterRequestDeleteEniEnum().BLOCK
		req.DeleteEni = &v
	}
	if in.DeleteELB {
		v := model.GetDeleteClusterRequestDeleteNetEnum().BLOCK
		req.DeleteNet = &v
	}
	if in.DeleteEFS {
		v := model.GetDeleteClusterRequestDeleteEfsEnum().BLOCK
		req.DeleteEfs = &v
	}
	switch in.OnDemandNodePolicy {
	case "reset":
		v := model.GetDeleteClusterRequestOndemandNodePolicyEnum().RESET
		req.OndemandNodePolicy = &v
	case "retain":
		v := model.GetDeleteClusterRequestOndemandNodePolicyEnum().RETAIN
		req.OndemandNodePolicy = &v
	default: // "delete" (official default)
		v := model.GetDeleteClusterRequestOndemandNodePolicyEnum().DELETE
		req.OndemandNodePolicy = &v
	}
	switch in.PeriodicNodePolicy {
	case "reset":
		v := model.GetDeleteClusterRequestPeriodicNodePolicyEnum().RESET
		req.PeriodicNodePolicy = &v
	case "retain":
		v := model.GetDeleteClusterRequestPeriodicNodePolicyEnum().RETAIN
		req.PeriodicNodePolicy = &v
	default:
		// Official default is "retain" (keep servers, data preserved) — leave
		// the parameter unset so the platform default applies (verified against
		// DeleteCluster.txt; the previous "reset" default was reversed).
	}
	// delete_obs / delete_sfs / delete_sfs30: official defaults are "skip",
	// which would leave OBS/SFS volumes behind. The provider always deletes
	// everything it manages (Q8), so request deletion explicitly when the
	// caller asks for it (kept minimal: not yet exposed on the input).
	if in.DeleteOBS {
		v := model.GetDeleteClusterRequestDeleteObsEnum().BLOCK
		req.DeleteObs = &v
	}
	if in.DeleteSFS {
		v := model.GetDeleteClusterRequestDeleteSfsEnum().BLOCK
		req.DeleteSfs = &v
	}
	if in.DeleteSFS30 {
		v := model.GetDeleteClusterRequestDeleteSfs30Enum().BLOCK
		req.DeleteSfs30 = &v
	}
	// Deletion is async (200 = job accepted); the controller polls ShowCluster
	// until the cluster is gone (verified live against real CCE — Q8).
	if _, err := s.cce.DeleteCluster(req); err != nil {
		if clouderrors.IsNotFound(err) {
			// Idempotent delete: already gone is success (aligns with
			// DeleteNodePool).
			return nil
		}
		return errors.Wrapf(err, "DeleteCluster %s failed", in.ClusterID)
	}
	return nil
}

// ShowQuotas implements Service (official ShowQuotas API; questionnaire Q7:
// prefer runtime quota values over documentation numbers).
func (s *Client) ShowQuotas(ctx context.Context) (*QuotaInfo, error) {
	resp, err := s.cce.ShowQuotas(&model.ShowQuotasRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ShowQuotas failed")
	}
	info := &QuotaInfo{}
	if resp.Quotas != nil {
		for _, r := range *resp.Quotas {
			if r.QuotaKey != nil && *r.QuotaKey == "cluster" {
				if r.QuotaLimit != nil {
					info.ClusterQuotaLimit = *r.QuotaLimit
				}
				if r.Used != nil {
					info.ClusterQuotaUsed = *r.Used
				}
			}
		}
	}
	return info, nil
}

// ListClusters implements Service. It lists all CCE clusters in the region,
// returning their ID, name and tags (used by the garbage collector's orphan
// sweeper).
func (s *Client) ListClusters(ctx context.Context) ([]ClusterRef, error) {
	resp, err := s.cce.ListClusters(&model.ListClustersRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ListClusters failed")
	}
	var refs []ClusterRef
	if resp.Items != nil {
		for _, c := range *resp.Items {
			ref := ClusterRef{}
			if c.Metadata != nil {
				if c.Metadata.Uid != nil {
					ref.ClusterID = *c.Metadata.Uid
				}
				ref.Name = c.Metadata.Name
			}
			ref.Tags = map[string]string{}
			if c.Spec != nil && c.Spec.ClusterTags != nil {
				for _, t := range *c.Spec.ClusterTags {
					if t.Key != nil && t.Value != nil {
						ref.Tags[*t.Key] = *t.Value
					}
				}
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// CreateAccessPolicy implements Service.
// ListEips implements Service.
func (s *Client) ListEips(ctx context.Context) ([]EipRef, error) {
	resp, err := s.eip.ListPublicips(&eipmodel.ListPublicipsRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ListPublicips failed")
	}
	var refs []EipRef
	if resp.Publicips != nil {
		for _, p := range *resp.Publicips {
			ref := EipRef{Tags: map[string]string{}}
			if p.Id != nil {
				ref.ID = *p.Id
			}
			if p.PublicIpAddress != nil {
				ref.Address = *p.PublicIpAddress
			}
			ref.Tags = parseKVTags(p.Tags)
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// DeleteEip implements Service.
func (s *Client) DeleteEip(ctx context.Context, eipID string) error {
	_, err := s.eip.DeletePublicip(&eipmodel.DeletePublicipRequest{PublicipId: eipID})
	if err != nil && !clouderrors.IsNotFound(err) {
		return errors.Wrapf(err, "DeletePublicip %s failed", eipID)
	}
	return nil
}

// ListVolumes implements Service.
func (s *Client) ListVolumes(ctx context.Context) ([]VolumeRef, error) {
	resp, err := s.evs.ListVolumes(&evsmodel.ListVolumesRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ListVolumes failed")
	}
	var refs []VolumeRef
	if resp.Volumes != nil {
		for _, v := range *resp.Volumes {
			refs = append(refs, VolumeRef{ID: v.Id, Name: v.Name, Tags: v.Tags})
		}
	}
	return refs, nil
}

// DeleteVolume implements Service.
func (s *Client) DeleteVolume(ctx context.Context, volumeID string) error {
	_, err := s.evs.DeleteVolume(&evsmodel.DeleteVolumeRequest{VolumeId: volumeID})
	if err != nil && !clouderrors.IsNotFound(err) {
		return errors.Wrapf(err, "DeleteVolume %s failed", volumeID)
	}
	return nil
}

// ListVpcs implements Service. VPC tags are not returned by ListVpcs, so this
// does an N+1 ShowVpcTags per VPC (VPC counts are small).
func (s *Client) ListVpcs(ctx context.Context) ([]VpcRef, error) {
	resp, err := s.vpc.ListVpcs(&vpcmodel.ListVpcsRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ListVpcs failed")
	}
	var refs []VpcRef
	if resp.Vpcs != nil {
		for _, v := range *resp.Vpcs {
			ref := VpcRef{ID: v.Id, Name: v.Name, Tags: map[string]string{}}
			if tags, terr := s.vpc.ShowVpcTags(&vpcmodel.ShowVpcTagsRequest{VpcId: v.Id}); terr == nil && tags.Tags != nil {
				for _, t := range *tags.Tags {
					ref.Tags[t.Key] = t.Value
				}
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// DeleteVpc implements Service.
func (s *Client) DeleteVpc(ctx context.Context, vpcID string) error {
	_, err := s.vpc.DeleteVpc(&vpcmodel.DeleteVpcRequest{VpcId: vpcID})
	if err != nil && !clouderrors.IsNotFound(err) {
		return errors.Wrapf(err, "DeleteVpc %s failed", vpcID)
	}
	return nil
}

// ListNatGateways implements Service. NAT tags are not returned by
// ListNatGateways, so this does an N+1 ShowNatGatewayTag per gateway.
func (s *Client) ListNatGateways(ctx context.Context) ([]NatGatewayRef, error) {
	resp, err := s.nat.ListNatGateways(&natmodel.ListNatGatewaysRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ListNatGateways failed")
	}
	var refs []NatGatewayRef
	if resp.NatGateways != nil {
		for _, gw := range *resp.NatGateways {
			if gw.Id == nil {
				continue
			}
			ref := NatGatewayRef{ID: *gw.Id, Tags: map[string]string{}}
			if gw.Name != nil {
				ref.Name = *gw.Name
			}
			if tags, terr := s.nat.ShowNatGatewayTag(&natmodel.ShowNatGatewayTagRequest{NatGatewayId: *gw.Id}); terr == nil && tags.Tags != nil {
				for _, t := range *tags.Tags {
					ref.Tags[t.Key] = t.Value
				}
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// DeleteNatGateway implements Service: SNAT rules first, then the gateway
// (the platform rejects deleting a gateway that still has SNAT rules).
func (s *Client) DeleteNatGateway(ctx context.Context, gatewayID string) error {
	ids := []string{gatewayID}
	if rules, err := s.nat.ListNatGatewaySnatRules(&natmodel.ListNatGatewaySnatRulesRequest{NatGatewayId: &ids}); err == nil && rules.SnatRules != nil {
		for _, r := range *rules.SnatRules {
			if _, derr := s.nat.DeleteNatGatewaySnatRule(&natmodel.DeleteNatGatewaySnatRuleRequest{
				NatGatewayId: gatewayID,
				SnatRuleId:   r.Id,
			}); derr != nil && !clouderrors.IsNotFound(derr) {
				return errors.Wrapf(derr, "DeleteNatGatewaySnatRule %s failed", r.Id)
			}
		}
	}
	_, err := s.nat.DeleteNatGateway(&natmodel.DeleteNatGatewayRequest{NatGatewayId: gatewayID})
	if err != nil && !clouderrors.IsNotFound(err) {
		return errors.Wrapf(err, "DeleteNatGateway %s failed", gatewayID)
	}
	return nil
}

// parseKVTags converts EIP "k=v" tag strings into a map.
func parseKVTags(tags *[]string) map[string]string {
	out := map[string]string{}
	if tags == nil {
		return out
	}
	for _, t := range *tags {
		if k, v, ok := strings.Cut(t, "="); ok {
			out[k] = v
		}
	}
	return out
}
func (s *Client) CreateAccessPolicy(ctx context.Context, in AccessPolicyInput) (string, error) {
	resp, err := s.cce.CreateAccessPolicy(&model.CreateAccessPolicyRequest{Body: toAccessPolicyModel(in)})
	if err != nil {
		return "", errors.Wrap(err, "CreateAccessPolicy failed")
	}
	if resp.PolicyId != nil {
		return *resp.PolicyId, nil
	}
	return "", nil
}

// UpdateAccessPolicy implements Service.
func (s *Client) UpdateAccessPolicy(ctx context.Context, policyID string, in AccessPolicyInput) error {
	_, err := s.cce.UpdateAccessPolicy(&model.UpdateAccessPolicyRequest{PolicyId: policyID, Body: toAccessPolicyModel(in)})
	if err != nil {
		return errors.Wrap(err, "UpdateAccessPolicy failed")
	}
	return nil
}

// ListAccessPolicies implements Service.
func (s *Client) ListAccessPolicies(ctx context.Context) ([]AccessPolicyInfo, error) {
	resp, err := s.cce.ListAccessPolicy(&model.ListAccessPolicyRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "ListAccessPolicy failed")
	}
	var infos []AccessPolicyInfo
	if resp.AccessPolicyList != nil {
		for _, p := range *resp.AccessPolicyList {
			info := AccessPolicyInfo{}
			if p.PolicyId != nil {
				info.PolicyID = *p.PolicyId
			}
			if p.Name != nil {
				info.Name = *p.Name
			}
			if p.PolicyType != nil {
				info.PolicyType = *p.PolicyType
			}
			if p.Principal != nil {
				info.PrincipalType = p.Principal.Type.Value()
				info.PrincipalIDs = p.Principal.Ids
			}
			if p.AccessScope != nil {
				info.Namespaces = p.AccessScope.Namespaces
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// DeleteAccessPolicy implements Service.
func (s *Client) DeleteAccessPolicy(ctx context.Context, policyID string) error {
	_, err := s.cce.DeleteAccessPolicy(&model.DeleteAccessPolicyRequest{PolicyId: policyID})
	if err != nil {
		return errors.Wrap(err, "DeleteAccessPolicy failed")
	}
	return nil
}

// toAccessPolicyModel maps an AccessPolicyInput to the CCE AccessPolicy model.
// An empty Namespaces list defaults to ["*"] (all namespaces).
func toAccessPolicyModel(in AccessPolicyInput) *model.AccessPolicy {
	kind := "AccessPolicy"
	apiVersion := "v3"
	namespaces := in.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{"*"}
	}
	principalType := model.GetPrincipalTypeEnum().USER
	switch in.PrincipalType {
	case "group":
		principalType = model.GetPrincipalTypeEnum().GROUP
	case "agency":
		principalType = model.GetPrincipalTypeEnum().AGENCY
	}
	return &model.AccessPolicy{
		Kind:       &kind,
		ApiVersion: &apiVersion,
		Name:       &in.Name,
		Clusters:   []string{in.ClusterID},
		AccessScope: &model.AccessScope{
			Namespaces: namespaces,
		},
		PolicyType: in.PolicyType,
		Principal: &model.Principal{
			Type: principalType,
			Ids:  in.PrincipalIDs,
		},
	}
}

// GetClusterKubeconfig implements Service. It downloads the cluster certificate
// via CreateKubernetesClusterCert and assembles a standard kubeconfig
// (mirrors the ACK provider's controller_kubeconfig.go approach).
func (s *Client) GetClusterKubeconfig(_ context.Context, clusterID string, durationDays int32) (string, error) {
	// Official duration semantics (CreateKubernetesClusterCert.txt): range
	// [1, 1827] days; -1 means the 5-year maximum (1827). Clamp anything
	// outside so the API is never called with an invalid value.
	switch {
	case durationDays == -1:
		durationDays = 1827
	case durationDays < 1:
		durationDays = 1
	case durationDays > 1827:
		durationDays = 1827
	}
	resp, err := s.cce.CreateKubernetesClusterCert(&model.CreateKubernetesClusterCertRequest{
		ClusterId: clusterID,
		Body:      &model.ClusterCertDuration{Duration: int32Ptr(durationDays)},
	})
	if err != nil {
		return "", errors.Wrap(err, "CreateKubernetesClusterCert failed")
	}
	return assembleKubeconfig(resp)
}

// CreateNodePool implements Service.
func (s *Client) CreateNodePool(ctx context.Context, in CreateNodePoolInput) (string, error) {
	if in.Flavor == "" {
		return "", errors.New("CreateNodePool: flavor is required")
	}
	if in.RootVolumeSize <= 0 {
		return "", errors.New("CreateNodePool: rootVolume size must be >= 40 GiB")
	}
	billingMode := model.GetNodeTemplateBillingModeEnum().E_0
	if in.BillingMode == 1 {
		billingMode = model.GetNodeTemplateBillingModeEnum().E_1
	}
	template := &model.NodeTemplate{
		Flavor:      stringPtr(in.Flavor),
		BillingMode: &billingMode,
		RootVolume:  &model.Volume{Size: in.RootVolumeSize, Volumetype: defaultString(in.RootVolumeType, "GPSSD")},
	}
	if in.OS != "" {
		template.Os = stringPtr(in.OS)
	}
	// Only set AZ when explicitly provided (empty AZ is rejected by CCE:
	// verified "Az [] is not in available az list").
	if in.AvailabilityZone != "" {
		template.Az = stringPtr(in.AvailabilityZone)
	}
	if len(in.DataVolumes) > 0 {
		volumes := make([]model.Volume, 0, len(in.DataVolumes))
		for _, v := range in.DataVolumes {
			volumes = append(volumes, model.Volume{Size: v.Size, Volumetype: defaultString(v.Type, "GPSSD")})
		}
		template.DataVolumes = &volumes
	}
	if in.SSHKey != "" {
		template.Login = &model.Login{SshKey: stringPtr(in.SSHKey)}
	}
	if in.EcsGroupId != "" {
		template.EcsGroupId = stringPtr(in.EcsGroupId)
	}
	if in.FaultDomain != "" {
		template.FaultDomain = stringPtr(in.FaultDomain)
	}
	if in.DedicatedHostId != "" {
		template.DedicatedHostId = stringPtr(in.DedicatedHostId)
	}
	if len(in.Taints) > 0 {
		taints, terr := parseTaints(in.Taints)
		if terr != nil {
			return "", terr
		}
		template.Taints = taints
	}
	if len(in.Labels) > 0 {
		template.K8sTags = in.Labels
	}
	template.UserTags = toUserTags(in.ClusterName, in.Tags)
	var extend *model.NodeExtendParam
	if in.Spot {
		// Spot (竞价) instances: marketType=spot, only effective with
		// billingMode=0 (on-demand). spotPrice defaults to the on-demand price
		// when empty (official NodeExtendParam).
		extend = &model.NodeExtendParam{}
		marketType := model.GetNodeExtendParamMarketTypeEnum().SPOT
		extend.MarketType = &marketType
		if in.SpotPrice != "" {
			extend.SpotPrice = stringPtr(in.SpotPrice)
		}
	}
	if in.PreInstall != "" || in.PostInstall != "" {
		// Node lifecycle hooks (preInstall/postInstall) are carried in
		// nodeTemplate.extendParam["alpha.cce/preInstall"|"alpha.cce/postInstall"];
		// values must already be base64-encoded (CCE requirement).
		if extend == nil {
			extend = &model.NodeExtendParam{}
		}
		if in.PreInstall != "" {
			extend.AlphaCcePreInstall = stringPtr(in.PreInstall)
		}
		if in.PostInstall != "" {
			extend.AlphaCcePostInstall = stringPtr(in.PostInstall)
		}
	}
	if extend != nil {
		template.ExtendParam = extend
	}
	if in.WaitPostInstallFinish != nil {
		// Wait for the postInstall script to finish before scheduling pods
		// onto the node (nodeTemplate.waitPostInstallFinish).
		template.WaitPostInstallFinish = in.WaitPostInstallFinish
	}
	spec := &model.NodePoolSpec{
		NodeTemplate:     template,
		InitialNodeCount: int32Ptr(in.InitialNodeCount),
	}
	if len(in.SecurityGroups) > 0 || len(in.CustomSecurityGroups) > 0 {
		groups := in.SecurityGroups
		if len(in.CustomSecurityGroups) > 0 {
			groups = in.CustomSecurityGroups
		}
		spec.CustomSecurityGroups = &groups
	}
	if in.Autoscaling != nil {
		spec.Autoscaling = toNodePoolAutoscaling(in.Autoscaling)
	}
	if len(in.ExtensionScaleGroups) > 0 {
		groups := make([]model.ExtensionScaleGroup, 0, len(in.ExtensionScaleGroups))
		for _, g := range in.ExtensionScaleGroups {
			groups = append(groups, model.ExtensionScaleGroup{
				Metadata: &model.ExtensionScaleGroupMetadata{Name: stringPtr(g.Name)},
				Spec: &model.ExtensionScaleGroupSpec{
					Flavor: stringPtr(g.Flavor),
					Az:     stringPtr(g.AvailabilityZone),
				},
			})
		}
		spec.ExtensionScaleGroups = &groups
	}
	pool := &model.NodePool{
		Kind:       "NodePool",
		ApiVersion: "v3",
		Metadata:   &model.NodePoolMetadata{Name: in.Name},
		Spec:       spec,
	}
	resp, err := s.cce.CreateNodePool(&model.CreateNodePoolRequest{
		ClusterId: in.ClusterID,
		Body:      pool,
	})
	if err != nil {
		// Idempotent create: if the pool already exists (a previous create
		// succeeded but the response was lost to throttling — the same failure
		// mode CreateCluster handles), adopt it by name instead of failing on a
		// 409 forever.
		if clouderrors.IsConflict(err) {
			if id, ferr := s.findNodePoolIDByName(ctx, in.ClusterID, in.Name); ferr == nil && id != "" {
				return id, nil
			}
		}
		return "", errors.Wrap(err, "CreateNodePool failed")
	}
	if resp.Metadata == nil || resp.Metadata.Uid == nil {
		return "", errors.New("CreateNodePool returned no node pool ID")
	}
	return *resp.Metadata.Uid, nil
}

// findNodePoolIDByName looks up a node pool ID by name within a cluster.
func (s *Client) findNodePoolIDByName(ctx context.Context, clusterID, name string) (string, error) {
	pools, err := s.ListNodePools(ctx, clusterID)
	if err != nil {
		return "", errors.Wrap(err, "ListNodePools failed")
	}
	for _, p := range pools {
		if p.Name == name {
			return p.NodePoolID, nil
		}
	}
	return "", errors.Errorf("node pool %q not found in cluster %s", name, clusterID)
}

// ScaleNodePool implements Service.
// NOTE: per the official API/SDK docs, desiredNodeCount is the ABSOLUTE
// expected total node count of the pool ("节点池期望节点数"; range 0 or a
// positive integer; omitting it defaults to 0 which deletes all nodes). The
// phrase "add to/subtract from the current count" in the SDK comment tells the
// CALLER how to compute the value to pass (target = current ± delta), it does
// not mean the API applies a delta. The user guide's "本次节点数与已有节点数
// 相加" phrasing is the console UX (the console computes the target for you).
// Final confirmation still requires a live test (questionnaire Q3: create a
// 2-node pool and pass desiredNodeCount=2 — absolute => stays 2, delta => 4).
func (s *Client) ScaleNodePool(_ context.Context, clusterID, nodePoolID string, desiredCount int32) error {
	if desiredCount < 0 {
		return errors.Errorf("ScaleNodePool: desired node count must be >= 0, got %d", desiredCount)
	}
	if _, err := s.cce.ScaleNodePool(&model.ScaleNodePoolRequest{
		ClusterId:  clusterID,
		NodepoolId: nodePoolID,
		Body: &model.ScaleNodePoolRequestBody{
			Kind:       "NodePool",
			ApiVersion: "v3",
			Spec: &model.ScaleNodePoolSpec{
				DesiredNodeCount: desiredCount,
				// "default" is the only scale group CCE supports today (official:
				// scaleGroups is required, the default group is named "default").
				ScaleGroups: []string{"default"},
			},
		},
	}); err != nil {
		// Idempotent scale: the platform rejects a scale to the count the pool
		// already has ("No scale task needed with desired node count N") — e.g.
		// right after creation (initialNodeCount == desired) or when a transient
		// 0 count races the scale. Treat it as success, not an error (verified
		// live).
		if clouderrors.IsScaleNoOp(err) {
			return nil
		}
		return errors.Wrapf(err, "ScaleNodePool %s failed", nodePoolID)
	}
	return nil
}

// UpdateNodePool implements Service. Official (cce_02_0356): initialNodeCount
// is required and defaults to 0 — omitting it shrinks the pool to 0, so the
// caller must set IgnoreInitialNodeCount=true to leave the count untouched.
func (s *Client) UpdateNodePool(_ context.Context, in UpdateNodePoolInput) error {
	spec := &model.NodePoolSpecUpdate{
		InitialNodeCount: in.InitialNodeCount,
	}
	if in.IgnoreInitialNodeCount {
		spec.IgnoreInitialNodeCount = boolPtr(true)
	}
	// Always send customSecurityGroups (empty slice = reset to default SG).
	spec.CustomSecurityGroups = &in.CustomSecurityGroups
	if in.Autoscaling != nil {
		spec.Autoscaling = toNodePoolAutoscaling(in.Autoscaling)
	}
	if in.TaintPolicyOnExistingNodes != "" {
		spec.TaintPolicyOnExistingNodes = stringPtr(in.TaintPolicyOnExistingNodes)
	}
	if in.LabelPolicyOnExistingNodes != "" {
		spec.LabelPolicyOnExistingNodes = stringPtr(in.LabelPolicyOnExistingNodes)
	}
	if in.UserTagsPolicyOnExistingNodes != "" {
		spec.UserTagsPolicyOnExistingNodes = stringPtr(in.UserTagsPolicyOnExistingNodes)
	}
	if _, err := s.cce.UpdateNodePool(&model.UpdateNodePoolRequest{
		ClusterId:  in.ClusterID,
		NodepoolId: in.NodePoolID,
		Body:       &model.NodePoolUpdate{Spec: spec},
	}); err != nil {
		return errors.Wrapf(err, "UpdateNodePool %s failed", in.NodePoolID)
	}
	return nil
}

// toNodePoolAutoscaling maps the provider-side autoscaling spec to the SDK
// model. A nil input is mapped to disabled (enable=false) so an explicit
// "disable autoscaling" update works.
// toNodePoolAutoscaling maps the provider-side autoscaling spec to the SDK
// model. Called only when the caller explicitly wants to set/change
// autoscaling; a nil input means "do not touch autoscaling" and is guarded
// here (callers also guard with `if in.Autoscaling != nil`).
func toNodePoolAutoscaling(in *NodePoolAutoscaling) *model.NodePoolNodeAutoscaling {
	if in == nil {
		return nil
	}
	return &model.NodePoolNodeAutoscaling{
		Enable:       boolPtr(in.Enable),
		MinNodeCount: int32Ptr(in.MinNodeCount),
		MaxNodeCount: int32Ptr(in.MaxNodeCount),
	}
}

// DeleteNodePool implements Service.
func (s *Client) DeleteNodePool(_ context.Context, clusterID, nodePoolID string) error {
	if _, err := s.cce.DeleteNodePool(&model.DeleteNodePoolRequest{
		ClusterId:  clusterID,
		NodepoolId: nodePoolID,
	}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, "DeleteNodePool %s failed", nodePoolID)
	}
	return nil
}

// ListNodePools implements Service.
func (s *Client) ListNodePools(_ context.Context, clusterID string) ([]NodePoolInfo, error) {
	resp, err := s.cce.ListNodePools(&model.ListNodePoolsRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrap(err, "ListNodePools failed")
	}
	var out []NodePoolInfo
	if resp.Items != nil {
		for _, p := range *resp.Items {
			info := NodePoolInfo{}
			if p.Metadata != nil {
				if p.Metadata.Uid != nil {
					info.NodePoolID = *p.Metadata.Uid
				}
				info.Name = p.Metadata.Name
			}
			if p.Spec != nil && p.Spec.InitialNodeCount != nil {
				info.DesiredNodeCount = *p.Spec.InitialNodeCount
			}
			// status.currentNode = expected total, status.activeNode = nodes
			// in Active state (official NodePoolStatus, verified live).
			if p.Status != nil {
				if p.Status.CurrentNode != nil {
					info.NodeCount = *p.Status.CurrentNode
				}
				if p.Status.ActiveNode != nil {
					info.ActiveNodeCount = *p.Status.ActiveNode
				}
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// ListNodes implements Service. It returns the provider IDs (node UIDs) of
// all nodes in the cluster. Each UID matches the spec.providerID of the
// corresponding workload node, which Cluster API uses to fill
// MachinePool.status.nodeRefs.
func (s *Client) ListNodes(_ context.Context, clusterID string) ([]string, error) {
	resp, err := s.cce.ListNodes(&model.ListNodesRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrap(err, "ListNodes failed")
	}
	var out []string
	if resp.Items != nil {
		for _, n := range *resp.Items {
			// ProviderID must match the huaweicloud cloud-provider contract:
			// `huaweicloud:///<serverId>` where serverId is the underlying ECS
			// instance ID (mirrors CAPA's aws:///az/instance-id, which uses the
			// underlying instance ID too). Verified against
			// kubernetes-sigs/cloud-provider-huaweicloud instances.go:
			// ProviderName="huaweicloud" + regexp ^huaweicloud:///([^/]+)$ +
			// InstanceID() = ecsClient.GetByNodeName().Id.
			if n.Status != nil && n.Status.ServerId != nil {
				out = append(out, "huaweicloud:///"+*n.Status.ServerId)
			}
		}
	}
	return out, nil
}

// ListNodesWithStatus implements Service.
func (s *Client) ListNodesWithStatus(_ context.Context, clusterID string) ([]NodeInfo, error) {
	resp, err := s.cce.ListNodes(&model.ListNodesRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrap(err, "ListNodes failed")
	}
	var out []NodeInfo
	if resp.Items != nil {
		for _, n := range *resp.Items {
			info := NodeInfo{}
			if n.Metadata != nil && n.Metadata.Uid != nil {
				info.UID = *n.Metadata.Uid
			}
			if n.Metadata != nil && n.Metadata.OwnerReferences != nil && n.Metadata.OwnerReferences.NodepoolID != nil {
				info.NodePoolID = *n.Metadata.OwnerReferences.NodepoolID
			}
			if n.Status != nil && n.Status.Phase != nil {
				info.Phase = n.Status.Phase.Value()
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// ResetNode implements Service. For node-pool nodes the spec is omitted so
// the platform revalidates and reinstalls with the node-pool configuration
// (official: "节点池内节点重置时不支持外部指定配置，将以节点池配置进行校验并重装").
func (s *Client) ResetNode(_ context.Context, clusterID string, nodeIDs []string) error {
	nodeList := make([]model.ResetNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		nodeList = append(nodeList, model.ResetNode{NodeID: id})
	}
	_, err := s.cce.ResetNode(&model.ResetNodeRequest{
		ClusterId: clusterID,
		Body:      &model.ResetNodeList{ApiVersion: "v3", Kind: "List", NodeList: nodeList},
	})
	if err != nil {
		return errors.Wrapf(err, "ResetNode for %d node(s) failed", len(nodeIDs))
	}
	return nil
}

// GetUpgradeInfo implements Service. Queries ShowClusterUpgradeInfo and maps
// the current release and the platform-offered target versions. An empty
// target list is a legitimate platform state (no upgrade path currently
// offered — questionnaire Q11, verified live).
func (s *Client) GetUpgradeInfo(_ context.Context, clusterID string) (*UpgradeInfo, error) {
	resp, err := s.cce.ShowClusterUpgradeInfo(&model.ShowClusterUpgradeInfoRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrap(err, "ShowClusterUpgradeInfo failed")
	}
	info := &UpgradeInfo{}
	if resp.Spec != nil && resp.Spec.VersionInfo != nil {
		vi := resp.Spec.VersionInfo
		if vi.Release != nil {
			info.CurrentVersion = *vi.Release
		}
		if vi.Patch != nil {
			info.Patch = *vi.Patch
		}
		if vi.SuggestPatch != nil {
			info.SuggestPatch = *vi.SuggestPatch
		}
		if vi.TargetVersions != nil {
			info.TargetVersions = *vi.TargetVersions
		}
	}
	return info, nil
}

// StartUpgrade implements Service. Drives the official upgrade orchestration:
// CreateUpgradeWorkFlow -> CreatePreCheck -> UpgradeCluster. Returns the
// upgrade task ID (UpgradeCluster response uid) used by ShowUpgradeTask.
//
// NOTE (verified live 2026-08-19): the platform enforces exact version
// consistency across the workflow and pre-check:
//   - clusterID must be set (SDK comment says it is server-generated, but the
//     API rejects an empty one: CCE_CM.0004 "Invalid field cluster ID");
//   - the cluster version must be the FULL "release-patch" form (e.g.
//     v1.33.12-r2), matching the workflow exactly, or the pre-check fails with
//     CCE_CM.0101.
func (s *Client) StartUpgrade(_ context.Context, clusterID, targetVersion string) (string, error) {
	// Resolve the full current version (release-patch) so the workflow and
	// pre-check carry identical values. A query failure is fatal — proceeding
	// with an empty version would only surface as a cryptic CCE_CM.0101.
	currentVersion := ""
	up, err := s.cce.ShowClusterUpgradeInfo(&model.ShowClusterUpgradeInfoRequest{ClusterId: clusterID})
	if err != nil {
		return "", errors.Wrap(err, "ShowClusterUpgradeInfo failed")
	}
	if up.Spec != nil && up.Spec.VersionInfo != nil {
		if vi := up.Spec.VersionInfo; vi.Release != nil {
			currentVersion = *vi.Release
			if vi.Patch != nil && *vi.Patch != "" {
				currentVersion += "-" + *vi.Patch
			}
		}
	}
	// 1. Create the upgrade workflow (targetVersion + clusterID are required).
	workflowBody := &model.CreateUpgradeWorkFlowRequestBody{
		Kind:       "WorkFlowTask",
		ApiVersion: "v3",
		Spec: &model.WorkFlowSpec{
			ClusterID:      stringPtr(clusterID),
			ClusterVersion: stringPtr(currentVersion),
			TargetVersion:  targetVersion,
		},
	}
	if _, err := s.cce.CreateUpgradeWorkFlow(&model.CreateUpgradeWorkFlowRequest{
		ClusterId: clusterID,
		Body:      workflowBody,
	}); err != nil {
		return "", errors.Wrap(err, "CreateUpgradeWorkFlow failed")
	}
	// 2. Run the pre-check (versions must match the workflow exactly).
	precheckBody := &model.PrecheckClusterRequestBody{
		ApiVersion: "v3",
		Kind:       "PreCheckTask",
		Spec: &model.PrecheckSpec{
			ClusterID:      stringPtr(clusterID),
			ClusterVersion: stringPtr(currentVersion),
			TargetVersion:  stringPtr(targetVersion),
		},
	}
	if _, err := s.cce.CreatePreCheck(&model.CreatePreCheckRequest{
		ClusterId: clusterID,
		Body:      precheckBody,
	}); err != nil {
		return "", errors.Wrap(err, "CreatePreCheck failed")
	}
	// 3. Execute the upgrade (strategy: in-place rolling update only —
	// official UpgradeStrategy constraint). userDefinedStep (batch size) is
	// REQUIRED under inPlaceRollingUpdate (verified live: CCE_CM.0004 "Field
	// user defined step must defined by inPlaceRollingUpdate strategy");
	// official range [1-60], default 20 (UpgradeCluster.txt; SDK says 1-40).
	upgradeResp, err := s.cce.UpgradeCluster(&model.UpgradeClusterRequest{
		ClusterId: clusterID,
		Body: &model.UpgradeClusterRequestBody{
			Metadata: &model.UpgradeClusterRequestMetadata{ApiVersion: "v3", Kind: "UpgradeTask"},
			Spec: &model.UpgradeSpec{
				ClusterUpgradeAction: &model.ClusterUpgradeAction{
					TargetVersion: targetVersion,
					Strategy: &model.UpgradeStrategy{
						Type: "inPlaceRollingUpdate",
						InPlaceRollingUpdate: &model.InPlaceRollingUpdate{
							UserDefinedStep: int32Ptr(20),
						},
					},
				},
			},
		},
	})
	if err != nil {
		return "", errors.Wrap(err, "UpgradeCluster failed")
	}
	if upgradeResp.Metadata == nil || upgradeResp.Metadata.Uid == nil {
		return "", errors.New("UpgradeCluster returned no task ID")
	}
	return *upgradeResp.Metadata.Uid, nil
}

// ShowUpgradeTask implements Service. Returns the upgrade task phase
// (Init/Queuing/Running/Pause/Success/Failed).
func (s *Client) ShowUpgradeTask(_ context.Context, clusterID, taskID string) (string, error) {
	resp, err := s.cce.ShowUpgradeClusterTask(&model.ShowUpgradeClusterTaskRequest{
		ClusterId: clusterID,
		TaskId:    taskID,
	})
	if err != nil {
		if clouderrors.IsNotFound(err) {
			// Task expired/gone: report a sentinel so the controller clears
			// the task ID instead of looping forever on a 404.
			return "", errors.Wrap(err, "upgrade task not found")
		}
		return "", errors.Wrap(err, "ShowUpgradeClusterTask failed")
	}
	if resp.Status == nil || resp.Status.Phase == nil {
		return "", errors.New("ShowUpgradeClusterTask returned no status phase")
	}
	return *resp.Status.Phase, nil
}

// CreateAddonInstance implements Service. Installs a CCE addon instance and
// returns its ID. Version is optional (empty = latest supported by the
// cluster).
func (s *Client) CreateAddonInstance(_ context.Context, in AddonInput) (string, error) {
	body := &model.InstanceRequest{
		Kind:       "Addon",
		ApiVersion: "v3",
		Metadata:   &model.AddonMetadata{Name: &in.Name},
		Spec: &model.InstanceRequestSpec{
			ClusterID:         in.ClusterID,
			AddonTemplateName: in.Name,
			Values:            in.Values,
		},
	}
	if in.Version != "" {
		body.Spec.Version = &in.Version
	}
	resp, err := s.cce.CreateAddonInstance(&model.CreateAddonInstanceRequest{Body: body})
	if err != nil {
		return "", errors.Wrapf(err, "CreateAddonInstance %s failed", in.Name)
	}
	if resp.Metadata == nil || resp.Metadata.Uid == nil {
		return "", errors.New("CreateAddonInstance returned no addon ID")
	}
	return *resp.Metadata.Uid, nil
}

// UpdateAddonInstance implements Service. Upgrades an addon instance to the
// given version (full install parameters should be supplied; omitted ones fall
// back to template defaults).
func (s *Client) UpdateAddonInstance(_ context.Context, in AddonInput) error {
	body := &model.InstanceRequest{
		Kind:       "Addon",
		ApiVersion: "v3",
		Metadata:   &model.AddonMetadata{Name: &in.Name},
		Spec: &model.InstanceRequestSpec{
			ClusterID:         in.ClusterID,
			AddonTemplateName: in.Name,
			Version:           &in.Version,
			Values:            in.Values,
		},
	}
	if _, err := s.cce.UpdateAddonInstance(&model.UpdateAddonInstanceRequest{
		Id:   in.AddonID,
		Body: body,
	}); err != nil {
		return errors.Wrapf(err, "UpdateAddonInstance %s failed", in.Name)
	}
	return nil
}

// ListAddonInstances implements Service.
func (s *Client) ListAddonInstances(_ context.Context, clusterID string) ([]AddonInfo, error) {
	resp, err := s.cce.ListAddonInstances(&model.ListAddonInstancesRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrap(err, "ListAddonInstances failed")
	}
	var out []AddonInfo
	if resp.Items != nil {
		for _, a := range *resp.Items {
			info := AddonInfo{}
			if a.Metadata != nil {
				if a.Metadata.Uid != nil {
					info.ID = *a.Metadata.Uid
				}
				if a.Metadata.Name != nil {
					info.Name = *a.Metadata.Name
				}
			}
			if a.Spec != nil {
				info.Version = a.Spec.Version
			}
			if a.Status != nil {
				info.Status = a.Status.Status.Value()
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// DeleteAddonInstance implements Service.
func (s *Client) DeleteAddonInstance(_ context.Context, _, addonID string) error {
	if _, err := s.cce.DeleteAddonInstance(&model.DeleteAddonInstanceRequest{Id: addonID}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, "DeleteAddonInstance %s failed", addonID)
	}
	return nil
}

// OwnedTagPrefix is the provider ownership tag key prefix, mirroring CAPA's
// owned-tag model. NOTE: CCE tag keys cannot contain "/" (official ResourceTag
// key charset is letters/digits/space/_.:=+-@, max 128), unlike AWS, so the
// key uses "." separators instead of the CAPA slash form. Used for idempotent
// addressing and future external-resource GC.
const OwnedTagPrefix = "cluster-api-provider-cce.cluster"

// ownedTagKey returns the ownership tag key for a cluster.
func ownedTagKey(clusterName string) string { return OwnedTagPrefix + "." + clusterName }

// ---- helpers ----

func assembleKubeconfig(resp *model.CreateKubernetesClusterCertResponse) (string, error) {
	if resp == nil || resp.Clusters == nil || len(*resp.Clusters) == 0 {
		return "", errors.New("kubeconfig response contains no clusters")
	}
	cfg := clientcmdapi.NewConfig()
	for _, c := range *resp.Clusters {
		if c.Name == nil || c.Cluster == nil {
			continue
		}
		server := ""
		if c.Cluster.Server != nil {
			server = *c.Cluster.Server
		}
		var ca []byte
		if c.Cluster.CertificateAuthorityData != nil {
			ca = decodeCertData(*c.Cluster.CertificateAuthorityData)
		}
		cfg.Clusters[*c.Name] = &clientcmdapi.Cluster{
			Server:                   server,
			CertificateAuthorityData: ca,
			InsecureSkipTLSVerify:    derefBool(c.Cluster.InsecureSkipTlsVerify),
		}
	}
	for _, u := range derefUsers(resp.Users) {
		if u.Name == nil || u.User == nil {
			continue
		}
		auth := &clientcmdapi.AuthInfo{}
		if u.User.ClientCertificateData != nil {
			auth.ClientCertificateData = decodeCertData(*u.User.ClientCertificateData)
		}
		if u.User.ClientKeyData != nil {
			auth.ClientKeyData = decodeCertData(*u.User.ClientKeyData)
		}
		cfg.AuthInfos[*u.Name] = auth
	}
	for _, ctx := range derefContexts(resp.Contexts) {
		if ctx.Name == nil || ctx.Context == nil {
			continue
		}
		cfg.Contexts[*ctx.Name] = &clientcmdapi.Context{
			Cluster:  derefString(ctx.Context.Cluster),
			AuthInfo: derefString(ctx.Context.User),
		}
	}
	if resp.CurrentContext != nil {
		cfg.CurrentContext = *resp.CurrentContext
	}
	data, err := clientcmd.Write(*cfg)
	if err != nil {
		return "", errors.Wrap(err, "failed to serialize kubeconfig")
	}
	return string(data), nil
}

// decodeCertData decodes a base64-encoded certificate/key field from the CCE
// cert API. The CCE CreateKubernetesClusterCert response returns
// certificate-authority-data / client-certificate-data / client-key-data as
// base64 of the PEM (verified live), but clientcmd expects raw bytes and
// base64-encodes them again on write — passing the string through un-decoded
// double-encodes the value and breaks `kubectl` ("unable to parse bytes as
// PEM block"). Decode failures fall back to the raw value for robustness.
func decodeCertData(s string) []byte {
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
		return dec
	}
	return []byte(s)
}

func derefUsers(in *[]model.Users) []model.Users {
	if in == nil {
		return nil
	}
	return *in
}

func derefContexts(in *[]model.Contexts) []model.Contexts {
	if in == nil {
		return nil
	}
	return *in
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func stringPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

func int32Ptr(i int32) *int32 { return &i }

// base64StrPtr base64-encodes a PEM string and returns a pointer; nil-safe
// (empty input -> nil so the CCE API omits the field).
func base64StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return stringPtr(base64.StdEncoding.EncodeToString([]byte(s)))
}

// encryptionModeEnum maps the spec string to the SDK enum.
func encryptionModeEnum(mode string) *model.EncryptionConfigMode {
	switch mode {
	case "KMS":
		m := model.GetEncryptionConfigModeEnum().KMS
		return &m
	default:
		m := model.GetEncryptionConfigModeEnum().DEFAULT
		return &m
	}
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func clusterCategory(category, networkMode string) *model.ClusterSpecCategory {
	if category == "Turbo" {
		c := model.GetClusterSpecCategoryEnum().TURBO
		return &c
	}
	if category == "" && networkMode == "eni" {
		// Official default: eni mode implies Turbo (CreateCluster.txt).
		c := model.GetClusterSpecCategoryEnum().TURBO
		return &c
	}
	c := model.GetClusterSpecCategoryEnum().CCE
	return &c
}

// parseTaint splits "key=value:effect" into its parts.
func parseTaints(in []string) (*[]model.Taint, error) {
	out := make([]model.Taint, 0, len(in))
	for _, t := range in {
		key, value, effect := parseTaint(t)
		if key == "" {
			return nil, errors.Errorf("invalid taint %q: key is required", t)
		}
		te, err := taintEffect(effect)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid taint %q", t)
		}
		taint := model.Taint{Key: key, Effect: te}
		if value != "" {
			taint.Value = stringPtr(value)
		}
		out = append(out, taint)
	}
	return &out, nil
}

func taintEffect(effect string) (model.TaintEffect, error) {
	switch effect {
	case "PreferNoSchedule":
		return model.GetTaintEffectEnum().PREFER_NO_SCHEDULE, nil
	case "NoExecute":
		return model.GetTaintEffectEnum().NO_EXECUTE, nil
	case "NoSchedule":
		return model.GetTaintEffectEnum().NO_SCHEDULE, nil
	default:
		return model.GetTaintEffectEnum().NO_SCHEDULE, errors.Errorf("unsupported taint effect %q (want NoSchedule|PreferNoSchedule|NoExecute)", effect)
	}
}

func parseTaint(s string) (key, value, effect string) {
	// Formats: "key=value:effect" or "key:effect". The effect follows the LAST
	// colon (a colon inside the value must not truncate it).
	rest := s
	if i := lastIndexByte(rest, ':'); i >= 0 {
		effect = rest[i+1:]
		rest = rest[:i]
	}
	if i := lastIndexByte(rest, '='); i >= 0 {
		key, value = rest[:i], rest[i+1:]
	} else {
		key = rest
	}
	if effect == "" {
		effect = "NoSchedule"
	}
	return key, value, effect
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// CreatePodIdentityAssociation implements Service.
func (s *Client) CreatePodIdentityAssociation(_ context.Context, in PodIdentityAssociationInput) (string, error) {
	resp, err := s.cce.CreatePodIdentityAssociation(&model.CreatePodIdentityAssociationRequest{
		ClusterId: in.ClusterID,
		Body: &model.PodIdentityAssociation{
			Namespace:      in.Namespace,
			ServiceAccount: in.ServiceAccount,
			AgencyName:     in.AgencyName,
		},
	})
	if err != nil {
		return "", errors.Wrapf(err, "CreatePodIdentityAssociation %s/%s failed", in.Namespace, in.ServiceAccount)
	}
	if resp.Uid == nil {
		return "", errors.New("CreatePodIdentityAssociation returned no association ID")
	}
	return *resp.Uid, nil
}

// ListPodIdentityAssociations implements Service.
func (s *Client) ListPodIdentityAssociations(_ context.Context, clusterID string) ([]PodIdentityAssociationInfo, error) {
	resp, err := s.cce.ListPodIdentityAssociations(&model.ListPodIdentityAssociationsRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrap(err, "ListPodIdentityAssociations failed")
	}
	var out []PodIdentityAssociationInfo
	if resp.Body != nil {
		for _, a := range *resp.Body {
			info := PodIdentityAssociationInfo{}
			if a.Uid != nil {
				info.ID = *a.Uid
			}
			if a.Namespace != nil {
				info.Namespace = *a.Namespace
			}
			if a.ServiceAccount != nil {
				info.ServiceAccount = *a.ServiceAccount
			}
			if a.AgencyName != nil {
				info.AgencyName = *a.AgencyName
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// UpgradeNodePool implements Service. Rolls the pool configuration onto
// existing nodes (official: maxUnavailable in [1,20] is the max nodes made
// unavailable per batch).
func (s *Client) UpgradeNodePool(_ context.Context, clusterID, nodePoolID string, maxUnavailable int32) error {
	if maxUnavailable < 1 || maxUnavailable > 20 {
		return errors.Errorf("UpgradeNodePool: maxUnavailable must be in [1,20], got %d", maxUnavailable)
	}
	kind := "NodePool"
	apiVersion := "v3"
	if _, err := s.cce.UpgradeNodePool(&model.UpgradeNodePoolRequest{
		ClusterId:  clusterID,
		NodepoolId: nodePoolID,
		Body: &model.UpgradeNodePool{
			Kind:       &kind,
			ApiVersion: &apiVersion,
			Spec:       &model.NodePoolUpgradeSpec{MaxUnavailable: int32Ptr(maxUnavailable)},
		},
	}); err != nil {
		return errors.Wrapf(err, "UpgradeNodePool %s failed", nodePoolID)
	}
	return nil
}

// DeletePodIdentityAssociation implements Service.
func (s *Client) DeletePodIdentityAssociation(_ context.Context, clusterID, associationID string) error {
	if _, err := s.cce.DeletePodIdentityAssociation(&model.DeletePodIdentityAssociationRequest{
		ClusterId:     clusterID,
		AssociationId: associationID,
	}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, "DeletePodIdentityAssociation %s failed", associationID)
	}
	return nil
}

// UpdateClusterLogConfig implements Service. Maps the declarative log spec to
// ClusterLogConfig (ttl_in_days 0-30 + log_configs[] with name/type/enable).
func (s *Client) UpdateClusterLogConfig(_ context.Context, clusterID string, ttlInDays int32, logs []LogConfigInput) error {
	if ttlInDays < 0 || ttlInDays > 30 {
		return errors.Errorf("UpdateClusterLogConfig: ttlInDays must be in [0,30], got %d", ttlInDays)
	}
	logConfigs := make([]model.ClusterLogConfigLogConfigs, 0, len(logs))
	for _, l := range logs {
		t := l.Type
		if t == "" {
			// Official default for control-plane components.
			t = "control"
		}
		logConfigs = append(logConfigs, model.ClusterLogConfigLogConfigs{
			Name:   stringPtr(l.Name),
			Enable: boolPtr(l.Enable),
			Type:   logConfigType(t),
		})
	}
	if _, err := s.cce.UpdateClusterLogConfig(&model.UpdateClusterLogConfigRequest{
		ClusterId: clusterID,
		Body: &model.ClusterLogConfig{
			TtlInDays:  int32Ptr(ttlInDays),
			LogConfigs: &logConfigs,
		},
	}); err != nil {
		return errors.Wrapf(err, "UpdateClusterLogConfig for %s failed", clusterID)
	}
	return nil
}

// ShowClusterLogConfig implements Service.
func (s *Client) ShowClusterLogConfig(_ context.Context, clusterID string) (*LogConfigInfo, error) {
	resp, err := s.cce.ShowClusterConfig(&model.ShowClusterConfigRequest{ClusterId: clusterID})
	if err != nil {
		return nil, errors.Wrapf(err, "ShowClusterConfig for %s failed", clusterID)
	}
	info := &LogConfigInfo{}
	if resp.TtlInDays != nil {
		info.TTLInDays = *resp.TtlInDays
	}
	if resp.LogConfigs != nil {
		for _, l := range *resp.LogConfigs {
			item := LogConfigInput{}
			if l.Name != nil {
				item.Name = *l.Name
			}
			if l.Enable != nil {
				item.Enable = *l.Enable
			}
			if l.Type != nil {
				item.Type = l.Type.Value()
			}
			info.Logs = append(info.Logs, item)
		}
	}
	return info, nil
}

// logConfigType maps a declarative log type to the SDK enum.
func logConfigType(t string) *model.ClusterLogConfigLogConfigsType {
	v := model.GetClusterLogConfigLogConfigsTypeEnum().CONTROL
	switch t {
	case "audit":
		v = model.GetClusterLogConfigLogConfigsTypeEnum().AUDIT
	case "system-addon":
		v = model.GetClusterLogConfigLogConfigsTypeEnum().SYSTEM_ADDON
	}
	return &v
}

// toClusterTags builds the CCE clusterTags array: the owned tag plus any
// user-supplied additional tags (user tags never override the owned tag).
func toClusterTags(clusterName string, userTags map[string]string) *[]model.ResourceTag {
	tags := []model.ResourceTag{{Key: stringPtr(ownedTagKey(clusterName)), Value: stringPtr("owned")}}
	for k, v := range userTags {
		if k == ownedTagKey(clusterName) {
			continue
		}
		tags = append(tags, model.ResourceTag{Key: stringPtr(k), Value: stringPtr(v)})
	}
	return &tags
}

// toUserTags builds the CCE node pool userTags array (owned tag + user tags).
func toUserTags(clusterName string, userTags map[string]string) *[]model.UserTag {
	tags := []model.UserTag{{Key: stringPtr(ownedTagKey(clusterName)), Value: stringPtr("owned")}}
	for k, v := range userTags {
		if k == ownedTagKey(clusterName) {
			continue
		}
		tags = append(tags, model.UserTag{Key: stringPtr(k), Value: stringPtr(v)})
	}
	return &tags
}
