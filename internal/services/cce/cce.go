/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package cce

import (
	"context"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// Client is the default CCE SDK-backed implementation of Service.
type Client struct {
	cce *ccev3.CceClient
}

// NewClient builds a CCE client from AK/SK and region.
// Pattern follows CAPHW pkg/scope/clients.go (SafeValueOf + Builder pattern).
func NewClient(regionID, ak, sk string) (*Client, error) {
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
	return &Client{cce: ccev3.NewCceClient(hcClient)}, nil
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
	spec := &model.ClusterSpec{
		Category:    clusterCategory(in.Category),
		Flavor:      stringPtr(in.Flavor),
		Version:     stringPtr(in.Version),
		BillingMode: int32Ptr(in.BillingMode),
		AgencyName:  stringPtr(in.AgencyName),
		ContainerNetwork: &model.ContainerNetwork{
			Mode: model.GetContainerNetworkModeEnum().OVERLAY_L2, // replaced below
		},
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
	if in.ServiceCIDR != "" {
		spec.ServiceNetwork = &model.ServiceNetwork{IPv4CIDR: stringPtr(in.ServiceCIDR)}
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
		// Empty whitelist defaults to 0.0.0.0/0 (official PublicAccess model).
		spec.PublicAccess = &model.PublicAccess{}
	}
	// TODO(P0): hostNetwork/authentication mapping and verification items —
	// see docs/research-sources.md questionnaire Q4/Q5.

	cluster := &model.Cluster{
		Kind:       "Cluster",
		ApiVersion: "v3",
		Metadata:   &model.ClusterMetadata{Name: in.Name},
		Spec:       spec,
	}
	resp, err := s.cce.CreateCluster(&model.CreateClusterRequest{Body: cluster})
	if err != nil {
		return "", errors.Wrap(err, "CreateCluster failed")
	}
	if resp.Metadata != nil && resp.Metadata.Uid != nil {
		return *resp.Metadata.Uid, nil
	}
	// Subscription (billingMode=1) cluster creates do NOT return a cluster ID
	// (official model_cluster_metadata.go note) — fall back to lookup by name.
	// TODO(P0): verify the created cluster phase before returning (Q1).
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
	case "retain":
		v := model.GetDeleteClusterRequestPeriodicNodePolicyEnum().RETAIN
		req.PeriodicNodePolicy = &v
	default: // "reset"
		v := model.GetDeleteClusterRequestPeriodicNodePolicyEnum().RESET
		req.PeriodicNodePolicy = &v
	}
	// TODO(P0): deletion is async (200 = job accepted); poll ShowCluster until
	// gone, with delete-status from the Job (questionnaire Q8).
	if _, err := s.cce.DeleteCluster(req); err != nil {
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

// GetClusterKubeconfig implements Service. It downloads the cluster certificate
// via CreateKubernetesClusterCert and assembles a standard kubeconfig
// (mirrors the ACK provider's controller_kubeconfig.go approach).
func (s *Client) GetClusterKubeconfig(_ context.Context, clusterID string, durationDays int32) (string, error) {
	if durationDays < 1 {
		durationDays = 1
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
func (s *Client) CreateNodePool(_ context.Context, in CreateNodePoolInput) (string, error) {
	template := &model.NodeTemplate{
		Flavor:     stringPtr(in.Flavor),
		Az:         stringPtr(in.AvailabilityZone),
		Os:         stringPtr(in.OS),
		RootVolume: &model.Volume{Size: in.RootVolumeSize, Volumetype: defaultString(in.RootVolumeType, "GPSSD")},
	}
	if in.DataVolumeSize > 0 {
		template.DataVolumes = &[]model.Volume{{Size: in.DataVolumeSize, Volumetype: defaultString(in.DataVolumeType, "GPSSD")}}
	}
	if in.SSHKey != "" {
		template.Login = &model.Login{SshKey: stringPtr(in.SSHKey)}
	}
	if len(in.Taints) > 0 {
		template.Taints = parseTaints(in.Taints)
	}
	if len(in.Labels) > 0 {
		template.K8sTags = in.Labels
	}
	spec := &model.NodePoolSpec{
		NodeTemplate:     template,
		InitialNodeCount: int32Ptr(in.InitialNodeCount),
	}
	if len(in.SecurityGroups) > 0 {
		spec.CustomSecurityGroups = &in.SecurityGroups
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
		return "", errors.Wrap(err, "CreateNodePool failed")
	}
	if resp.Metadata == nil || resp.Metadata.Uid == nil {
		return "", errors.New("CreateNodePool returned no node pool ID")
	}
	return *resp.Metadata.Uid, nil
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
	if _, err := s.cce.ScaleNodePool(&model.ScaleNodePoolRequest{
		ClusterId:  clusterID,
		NodepoolId: nodePoolID,
		Body: &model.ScaleNodePoolRequestBody{
			Kind:       "NodePool",
			ApiVersion: "v3",
			Spec: &model.ScaleNodePoolSpec{
				DesiredNodeCount: desiredCount,
				ScaleGroups:      []string{"default"},
			},
		},
	}); err != nil {
		return errors.Wrapf(err, "ScaleNodePool %s failed", nodePoolID)
	}
	return nil
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
			out = append(out, info)
		}
	}
	return out, nil
}

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
			ca = []byte(*c.Cluster.CertificateAuthorityData)
		}
		cfg.Clusters[*c.Name] = &clientcmdapi.Cluster{Server: server, CertificateAuthorityData: ca}
	}
	for _, u := range derefUsers(resp.Users) {
		if u.Name == nil || u.User == nil {
			continue
		}
		auth := &clientcmdapi.AuthInfo{}
		if u.User.ClientCertificateData != nil {
			auth.ClientCertificateData = []byte(*u.User.ClientCertificateData)
		}
		if u.User.ClientKeyData != nil {
			auth.ClientKeyData = []byte(*u.User.ClientKeyData)
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

func stringPtr(s string) *string { return &s }

func int32Ptr(i int32) *int32 { return &i }

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func clusterCategory(category string) *model.ClusterSpecCategory {
	if category == "Turbo" {
		c := model.GetClusterSpecCategoryEnum().TURBO
		return &c
	}
	c := model.GetClusterSpecCategoryEnum().CCE
	return &c
}

// parseTaint splits "key=value:effect" into its parts.
func parseTaints(in []string) *[]model.Taint {
	out := make([]model.Taint, 0, len(in))
	for _, t := range in {
		key, value, effect := parseTaint(t)
		if key == "" {
			continue
		}
		taint := model.Taint{Key: key, Effect: taintEffect(effect)}
		if value != "" {
			taint.Value = stringPtr(value)
		}
		out = append(out, taint)
	}
	return &out
}

func taintEffect(effect string) model.TaintEffect {
	switch effect {
	case "PreferNoSchedule":
		return model.GetTaintEffectEnum().PREFER_NO_SCHEDULE
	case "NoExecute":
		return model.GetTaintEffectEnum().NO_EXECUTE
	default:
		return model.GetTaintEffectEnum().NO_SCHEDULE
	}
}

func parseTaint(s string) (key, value, effect string) {
	// Formats: "key=value:effect" or "key:effect".
	rest := s
	if i := indexByte(rest, ':'); i >= 0 {
		effect = rest[i+1:]
		rest = rest[:i]
	}
	if i := indexByte(rest, '='); i >= 0 {
		key, value = rest[:i], rest[i+1:]
	} else {
		key = rest
	}
	if effect == "" {
		effect = "NoSchedule"
	}
	return key, value, effect
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
