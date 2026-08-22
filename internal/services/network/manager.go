/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package network

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
	natv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2"
	natmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/model"
	natregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/region"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
	"github.com/pkg/errors"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// Managed-network constants (defaults mirror hack/smoke-setup and
// hack/nat-egress, which were verified against a real account).
const (
	defaultVPCCIDR        = "10.0.0.0/16"
	defaultNatGatewaySpec = "1"
	natActiveTimeout      = 60 * time.Second
	pollInterval          = 5 * time.Second
	eipBandwidthSize      = 5
)

// ownedTagPrefix mirrors cceService.OwnedTagPrefix ("cluster-api-provider-cce.cluster").
// Kept local to avoid a network -> cce import for a single constant.
const ownedTagPrefix = "cluster-api-provider-cce.cluster"

// HasOwnedTag reports whether tags carry the provider owned tag for
// clusterName (cluster-api-provider-cce.cluster.<name>=owned), the CAPA
// adoption marker.
func HasOwnedTag(tags common.Tags, clusterName string) bool {
	return tags[ownedTagPrefix+"."+clusterName] == "owned"
}

// ownedTagKey returns the provider owned tag key for clusterName
// (cluster-api-provider-cce.cluster.<name>).
func ownedTagKey(clusterName string) string {
	return ownedTagPrefix + "." + clusterName
}

// IsManaged reports whether the network spec asks the provider to own the
// VPC/subnets/NAT. Two managed forms (mirrors CAPA):
//   - create: vpc.id empty with a cidr (create) or a recorded ResourceID;
//   - adopt:  vpc.id set with the owned tag (managed, including deletion).
//
// BYO (vpc.id set without the owned tag) is not managed.
func IsManaged(spec *common.NetworkSpec, clusterName string) bool {
	if spec == nil {
		return false
	}
	if spec.VPC.ID == "" {
		return spec.VPC.ResourceID != "" || spec.VPC.CIDR != ""
	}
	return HasOwnedTag(spec.VPC.Tags, clusterName)
}

// ManagerInterface is the managed-network surface used by controllers; tests
// inject fakes (pattern mirrors ValidatorInterface).
type ManagerInterface interface {
	// ReconcileVpc ensures the managed VPC exists (or adopts an existing
	// owned-tagged VPC) and prepares the default subnet spec, backfilling
	// ResourceID.
	ReconcileVpc(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	// ReconcileSubnets ensures the managed subnets exist, backfilling
	// ResourceID/NeutronSubnetID.
	ReconcileSubnets(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	// ReconcileNatGateway ensures the NAT gateway + EIP + SNAT rules exist
	// (no-op when natGateway is nil or disabled).
	ReconcileNatGateway(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
	// DeleteNetwork tears down the provider-managed network in dependency
	// order (SNAT -> NAT -> EIP -> subnets -> VPC), aggregating per-resource
	// errors so one failure does not block the rest. BYO specs are a no-op.
	DeleteNetwork(ctx context.Context, spec *common.NetworkSpec, clusterName string) error
}

var _ ManagerInterface = (*Manager)(nil)

// Manager creates and deletes provider-owned network resources through the
// VPC/NAT/EIP APIs.
type Manager struct {
	vpc *vpcv2.VpcClient
	nat *natv2.NatClient
	eip *eipv2.EipClient
}

// NewManager builds a VPC/NAT/EIP API-backed network manager.
func NewManager(regionID, ak, sk string) (*Manager, error) {
	region, err := vpcregion.SafeValueOf(regionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve VPC region %q", regionID)
	}
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build network credentials")
	}
	httpConfig := config.DefaultHttpConfig()

	vpcHC, err := vpcv2.VpcClientBuilder().WithRegion(region).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build VPC client")
	}
	natRegion, err := natregion.SafeValueOf(regionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve NAT region %q", regionID)
	}
	natHC, err := natv2.NatClientBuilder().WithRegion(natRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build NAT client")
	}
	eipRegion, err := eipregion.SafeValueOf(regionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve EIP region %q", regionID)
	}
	eipHC, err := eipv2.EipClientBuilder().WithRegion(eipRegion).WithCredential(cred).WithHttpConfig(httpConfig).SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build EIP client")
	}
	return &Manager{vpc: vpcv2.NewVpcClient(vpcHC), nat: natv2.NewNatClient(natHC), eip: eipv2.NewEipClient(eipHC)}, nil
}

// ReconcileVpc implements ManagerInterface.
func (m *Manager) ReconcileVpc(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	if spec.VPC.ID != "" {
		// vpc.id set: BYO (no owned tag) is a no-op; adopted (owned tag) is
		// referenced and its subnets/NAT are still reconciled below.
		if !HasOwnedTag(spec.VPC.Tags, clusterName) {
			return nil
		}
		spec.VPC.ResourceID = spec.VPC.ID
		ensureDefaultSubnet(spec, clusterName)
		return nil
	}
	if err := m.ensureVpc(ctx, spec, clusterName); err != nil {
		return err
	}
	ensureDefaultSubnet(spec, clusterName)
	return nil
}

// ReconcileSubnets implements ManagerInterface.
func (m *Manager) ReconcileSubnets(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	return m.ensureSubnets(ctx, spec, clusterName)
}

// ReconcileNatGateway implements ManagerInterface.
func (m *Manager) ReconcileNatGateway(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	if spec.NatGateway == nil {
		return nil
	}
	return m.ensureNatGateway(ctx, spec, clusterName)
}

// DeleteNetwork implements ManagerInterface.
func (m *Manager) DeleteNetwork(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	if spec.VPC.ID != "" && !HasOwnedTag(spec.VPC.Tags, clusterName) {
		return nil // BYO: referenced, never deleted.
	}
	// managed (vpc.id empty) or adopted (vpc.id + owned tag): both are torn
	// down. Adopted VPCs use the referenced id, managed VPCs the ResourceID.
	vpcID := spec.VPC.ResourceID
	if vpcID == "" {
		vpcID = spec.VPC.ID
	}
	if vpcID == "" {
		return nil // never created.
	}
	var errs []error
	if ng := spec.NatGateway; ng != nil && (ng.ResourceID != "" || ng.EIPResourceID != "") {
		gwID := ng.ResourceID
		if gwID == "" {
			// Resource IDs may have been lost (spec patch failure after
			// creation); fall back to name discovery so the gateway does
			// not leak (mirrors CAPA's describe-based deletion).
			if id := m.findNatGatewayByName(ctx, clusterName+"-nat"); id != "" {
				gwID = id
			}
		}
		// Resolve the EIP BEFORE deleting SNAT rules: the rules carry the EIP
		// ID and are removed first, so a late lookup would find nothing.
		eipID := ng.EIPResourceID
		if eipID == "" && gwID != "" {
			if id := m.findEipBySnatRules(ctx, gwID); id != "" {
				eipID = id
			}
		}
		if gwID != "" {
			if err := m.deleteSnatRules(ctx, gwID); err != nil {
				errs = append(errs, errors.Wrap(err, "delete SNAT rules"))
			}
			if err := m.deleteNatGateway(ctx, gwID); err != nil {
				errs = append(errs, errors.Wrapf(err, "delete NAT gateway %s", gwID))
			}
		}
		if eipID != "" {
			if err := m.deleteEip(ctx, eipID); err != nil {
				errs = append(errs, errors.Wrapf(err, "delete EIP %s", eipID))
			}
		}
	}
	for i := range spec.Subnets {
		s := &spec.Subnets[i]
		if s.ResourceID == "" {
			continue
		}
		if err := m.deleteSubnet(ctx, vpcID, s.ResourceID); err != nil {
			errs = append(errs, errors.Wrapf(err, "delete subnet %s", s.ResourceID))
		}
	}
	if err := m.deleteVpc(ctx, vpcID); err != nil {
		errs = append(errs, errors.Wrapf(err, "delete VPC %s", vpcID))
	}
	return joinErrors(errs)
}

// ---- reconcile helpers ----

// ensureVpc creates the VPC (or re-discovers it by name after a lost spec
// patch) and backfills spec.VPC.ResourceID.
func (m *Manager) ensureVpc(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	if spec.VPC.ResourceID != "" {
		return nil
	}
	name := spec.VPC.Name
	if name == "" {
		name = clusterName + "-vpc"
	}
	if id := m.findVpcByName(ctx, name); id != "" {
		spec.VPC.ResourceID = id
		return nil
	}
	cidr := spec.VPC.CIDR
	if cidr == "" {
		cidr = defaultVPCCIDR
	}
	// Managed VPCs carry the provider owned tag (key*value star format, verified
	// live: ShowVpcTags splits on '*' — the equal sign is NOT a separator).
	ownedTag := []string{ownedTagKey(clusterName) + "*owned"}
	resp, err := m.vpc.CreateVpc(&vpcmodel.CreateVpcRequest{Body: &vpcmodel.CreateVpcRequestBody{
		Vpc: &vpcmodel.CreateVpcOption{Name: strPtr(name), Cidr: strPtr(cidr), Description: strPtr(spec.VPC.Description), Tags: &ownedTag},
	}})
	if err != nil {
		return errors.Wrapf(err, "CreateVpc %q failed", name)
	}
	if resp.Vpc == nil || resp.Vpc.Id == "" {
		return errors.Errorf("CreateVpc %q returned no id", name)
	}
	spec.VPC.ResourceID = resp.Vpc.Id
	return nil
}

// ensureDefaultSubnet derives a default node subnet from the VPC CIDR when
// the spec lists none (in-spec so the derived values persist and later
// reconciles stay idempotent).
func ensureDefaultSubnet(spec *common.NetworkSpec, clusterName string) {
	if len(spec.Subnets) > 0 {
		return
	}
	spec.Subnets = []common.Subnet{{
		Name: clusterName + "-subnet-node",
		CIDR: firstSlash24(spec.VPC.CIDR),
		Type: common.SubnetTypeNode,
	}}
}

// ensureSubnets creates the managed subnets (or re-discovers them by name)
// and backfills ResourceID/NeutronSubnetID. BYO subnets (id set) are skipped.
func (m *Manager) ensureSubnets(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	if len(spec.Subnets) == 0 {
		return errors.New("managed network requires at least one subnet")
	}
	existing := m.listSubnets(ctx, spec.VPC.ResourceID)
	for i := range spec.Subnets {
		s := &spec.Subnets[i]
		if s.ID != "" || s.ResourceID != "" {
			continue
		}
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("%s-subnet-%d", clusterName, i)
		}
		for _, sub := range existing {
			if sub.Name == name {
				s.ResourceID = sub.Id
				s.NeutronSubnetID = sub.NeutronSubnetId
				break
			}
		}
		if s.ResourceID != "" {
			continue
		}
		if s.CIDR == "" {
			return errors.Errorf("subnet %q has no cidr (managed subnets require one)", name)
		}
		resp, err := m.vpc.CreateSubnet(&vpcmodel.CreateSubnetRequest{Body: &vpcmodel.CreateSubnetRequestBody{
			Subnet: &vpcmodel.CreateSubnetOption{
				Name:             name,
				Cidr:             s.CIDR,
				VpcId:            spec.VPC.ResourceID,
				GatewayIp:        gatewayIP(s.CIDR),
				AvailabilityZone: strPtr(s.AvailabilityZone),
			},
		}})
		if err != nil {
			return errors.Wrapf(err, "CreateSubnet %q failed", name)
		}
		if resp.Subnet == nil || resp.Subnet.Id == "" {
			return errors.Errorf("CreateSubnet %q returned no id", name)
		}
		s.ResourceID = resp.Subnet.Id
		s.NeutronSubnetID = resp.Subnet.NeutronSubnetId
	}
	return nil
}

// ensureNatGateway creates the EIP + NAT gateway (or re-discovers it by
// name), waits for ACTIVE and reconciles one SNAT rule per managed node
// subnet.
func (m *Manager) ensureNatGateway(ctx context.Context, spec *common.NetworkSpec, clusterName string) error {
	ng := spec.NatGateway
	if ng.ResourceID == "" {
		name := clusterName + "-nat"
		if id := m.findNatGatewayByName(ctx, name); id != "" {
			ng.ResourceID = id
		}
	}
	if ng.ResourceID == "" {
		if ng.EIPResourceID == "" {
			id, err := m.createEip(ctx, clusterName+"-nat-eip", clusterName)
			if err != nil {
				return err
			}
			ng.EIPResourceID = id
		}
		subnetID := firstManagedNodeSubnet(spec)
		if subnetID == "" {
			return errors.New("natGateway requires a managed node subnet")
		}
		gwID, err := m.createNatGateway(ctx, clusterName+"-nat", spec.VPC.ResourceID, subnetID, ng.Spec, clusterName)
		if err != nil {
			return err
		}
		ng.ResourceID = gwID
	}
	if err := m.waitNatGatewayActive(ctx, ng.ResourceID); err != nil {
		return err
	}
	if ng.EIPResourceID == "" {
		if id := m.findEipBySnatRules(ctx, ng.ResourceID); id != "" {
			ng.EIPResourceID = id
		}
	}
	return m.ensureSnatRules(ctx, spec, ng)
}

// ensureSnatRules reconciles one SNAT rule per managed node subnet (BYO and
// ENI subnets are skipped: ENI subnets carry container traffic, not node
// egress).
func (m *Manager) ensureSnatRules(ctx context.Context, spec *common.NetworkSpec, ng *common.NatGatewaySpec) error {
	rules := m.listSnatRules(ctx, ng.ResourceID)
	existing := map[string]bool{}
	for _, r := range rules {
		existing[r.NetworkId] = true
	}
	for i := range spec.Subnets {
		s := &spec.Subnets[i]
		if s.ID != "" || s.Type == common.SubnetTypeENI || s.ResourceID == "" || existing[s.ResourceID] {
			continue
		}
		if _, err := m.nat.CreateNatGatewaySnatRule(&natmodel.CreateNatGatewaySnatRuleRequest{
			Body: &natmodel.CreateNatGatewaySnatRuleRequestOption{
				SnatRule: &natmodel.CreateNatGatewaySnatRuleOption{
					NatGatewayId: ng.ResourceID,
					NetworkId:    &s.ResourceID,
					SourceType:   int32Ptr(0),
					FloatingIpId: ng.EIPResourceID,
				},
			},
		}); err != nil {
			return errors.Wrapf(err, "CreateNatGatewaySnatRule for subnet %s failed", s.ResourceID)
		}
	}
	return nil
}

// ---- cloud query helpers ----

func (m *Manager) findVpcByName(ctx context.Context, name string) string {
	resp, err := m.vpc.ListVpcs(&vpcmodel.ListVpcsRequest{})
	if err != nil || resp.Vpcs == nil {
		return ""
	}
	for _, v := range *resp.Vpcs {
		if v.Name == name {
			return v.Id
		}
	}
	return ""
}

func (m *Manager) listSubnets(ctx context.Context, vpcID string) []vpcmodel.Subnet {
	vpc := vpcID
	resp, err := m.vpc.ListSubnets(&vpcmodel.ListSubnetsRequest{VpcId: &vpc})
	if err != nil || resp.Subnets == nil {
		return nil
	}
	return *resp.Subnets
}

// findNatGatewayByName returns the NAT gateway ID with the given name, or
// nil when no gateway carries it.
func (m *Manager) findNatGatewayByName(ctx context.Context, name string) string {
	gw, err := m.findNatGateway(ctx, name)
	if err != nil || gw == nil {
		return ""
	}
	return *gw
}

func (m *Manager) findNatGateway(ctx context.Context, name string) (*string, error) {
	resp, err := m.nat.ListNatGateways(&natmodel.ListNatGatewaysRequest{Name: &name})
	if err != nil {
		return nil, errors.Wrapf(err, "ListNatGateways %q failed", name)
	}
	if resp.NatGateways == nil || len(*resp.NatGateways) == 0 {
		return nil, nil
	}
	for _, gw := range *resp.NatGateways {
		if gw.Id != nil && gw.Name != nil && *gw.Name == name {
			return gw.Id, nil
		}
	}
	return nil, nil
}

type snatRule struct {
	ID           string
	NetworkId    string
	FloatingIpID string
}

func (m *Manager) listSnatRules(ctx context.Context, gatewayID string) []snatRule {
	ids := []string{gatewayID}
	resp, err := m.nat.ListNatGatewaySnatRules(&natmodel.ListNatGatewaySnatRulesRequest{NatGatewayId: &ids})
	if err != nil || resp.SnatRules == nil {
		return nil
	}
	var out []snatRule
	for _, r := range *resp.SnatRules {
		out = append(out, snatRule{ID: r.Id, NetworkId: r.NetworkId, FloatingIpID: r.FloatingIpId})
	}
	return out
}

// findEipBySnatRules extracts the EIP ID bound to the gateway's first SNAT
// rule (used to re-adopt an EIP whose ID was lost).
func (m *Manager) findEipBySnatRules(ctx context.Context, gatewayID string) string {
	for _, r := range m.listSnatRules(ctx, gatewayID) {
		if r.FloatingIpID != "" {
			return r.FloatingIpID
		}
	}
	return ""
}

func (m *Manager) createEip(ctx context.Context, name, clusterName string) (string, error) {
	shareType := eipmodel.GetCreatePublicipBandwidthOptionShareTypeEnum().PER
	size := int32(eipBandwidthSize)
	bandwidth := eipmodel.CreatePublicipBandwidthOption{ShareType: shareType, Name: &name, Size: &size}
	publicip := eipmodel.CreatePublicipOption{Type: "5_bgp", Alias: &name}
	resp, err := m.eip.CreatePublicip(&eipmodel.CreatePublicipRequest{Body: &eipmodel.CreatePublicipRequestBody{
		Bandwidth: &bandwidth,
		Publicip:  &publicip,
	}})
	if err != nil {
		return "", errors.Wrapf(err, "CreatePublicip %q failed", name)
	}
	if resp.Publicip == nil || resp.Publicip.Id == nil {
		return "", errors.Errorf("CreatePublicip %q returned no id", name)
	}
	eipID := *resp.Publicip.Id
	// EIP has no tags on the create call; tag it separately (CreatePublicipTag,
	// {Key,Value} structured — key ≤128, official 2026-08-05). The GC sweeper
	// relies on this owned tag to find orphaned EIPs.
	if _, err := m.eip.CreatePublicipTag(&eipmodel.CreatePublicipTagRequest{
		PublicipId: eipID,
		Body:       &eipmodel.CreatePublicipTagRequestBody{Tag: &eipmodel.ResourceTagOption{Key: ownedTagKey(clusterName), Value: "owned"}},
	}); err != nil {
		return "", errors.Wrapf(err, "CreatePublicipTag on %s failed", eipID)
	}
	return eipID, nil
}

func (m *Manager) createNatGateway(ctx context.Context, name, vpcID, subnetID, spec, clusterName string) (string, error) {
	// Managed NAT gateways carry the provider owned tag (same star format as
	// VPC — inferred, unverified live due to account balance; the independent
	// CreateNatGatewayTag API is the fallback).
	ownedTag := []string{ownedTagKey(clusterName) + "*owned"}
	resp, err := m.nat.CreateNatGateway(&natmodel.CreateNatGatewayRequest{
		Body: &natmodel.CreateNatGatewayRequestBody{
			NatGateway: &natmodel.CreateNatGatewayOption{
				Name:              name,
				RouterId:          vpcID,
				InternalNetworkId: subnetID,
				Spec:              natSpecEnum(spec),
				Tags:              &ownedTag,
			},
		},
	})
	if err != nil {
		return "", errors.Wrapf(err, "CreateNatGateway %q failed", name)
	}
	if resp.NatGatewayId != nil {
		return *resp.NatGatewayId, nil
	}
	if resp.NatGateway != nil && resp.NatGateway.Id != nil {
		return *resp.NatGateway.Id, nil
	}
	return "", errors.Errorf("CreateNatGateway %q returned no id", name)
}

func (m *Manager) waitNatGatewayActive(ctx context.Context, gatewayID string) error {
	deadline := time.Now().Add(natActiveTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := m.nat.ShowNatGateway(&natmodel.ShowNatGatewayRequest{NatGatewayId: gatewayID})
		if err != nil {
			return errors.Wrapf(err, "ShowNatGateway %s failed", gatewayID)
		}
		if resp.NatGateway != nil && resp.NatGateway.Status != nil && resp.NatGateway.Status.Value() == "ACTIVE" {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return errors.Errorf("NAT gateway %s not ACTIVE within %s", gatewayID, natActiveTimeout)
}

// ---- delete helpers (NotFound-tolerant, aggregated by the caller) ----

func (m *Manager) deleteSnatRules(ctx context.Context, gatewayID string) error {
	for _, r := range m.listSnatRules(ctx, gatewayID) {
		if _, err := m.nat.DeleteNatGatewaySnatRule(&natmodel.DeleteNatGatewaySnatRuleRequest{
			NatGatewayId: gatewayID,
			SnatRuleId:   r.ID,
		}); err != nil && !clouderrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (m *Manager) deleteNatGateway(ctx context.Context, gatewayID string) error {
	if _, err := m.nat.ShowNatGateway(&natmodel.ShowNatGatewayRequest{NatGatewayId: gatewayID}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err := m.nat.DeleteNatGateway(&natmodel.DeleteNatGatewayRequest{NatGatewayId: gatewayID})
	if err != nil && clouderrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *Manager) deleteEip(ctx context.Context, eipID string) error {
	if _, err := m.eip.ShowPublicip(&eipmodel.ShowPublicipRequest{PublicipId: eipID}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err := m.eip.DeletePublicip(&eipmodel.DeletePublicipRequest{PublicipId: eipID})
	if err != nil && clouderrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *Manager) deleteSubnet(ctx context.Context, vpcID, subnetID string) error {
	if _, err := m.vpc.ShowSubnet(&vpcmodel.ShowSubnetRequest{SubnetId: subnetID}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err := m.vpc.DeleteSubnet(&vpcmodel.DeleteSubnetRequest{VpcId: vpcID, SubnetId: subnetID})
	if err != nil && clouderrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *Manager) deleteVpc(ctx context.Context, vpcID string) error {
	if _, err := m.vpc.ShowVpc(&vpcmodel.ShowVpcRequest{VpcId: vpcID}); err != nil {
		if clouderrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err := m.vpc.DeleteVpc(&vpcmodel.DeleteVpcRequest{VpcId: vpcID})
	if err != nil && clouderrors.IsNotFound(err) {
		return nil
	}
	return err
}

// ---- pure helpers ----

// firstManagedNodeSubnet returns the ResourceID of the first managed node
// subnet (the NAT gateway must be anchored to a subnet).
func firstManagedNodeSubnet(spec *common.NetworkSpec) string {
	for _, s := range spec.Subnets {
		if s.ID == "" && s.Type != common.SubnetTypeENI && s.ResourceID != "" {
			return s.ResourceID
		}
	}
	return ""
}

// firstSlash24 returns the first /24 inside vpcCIDR (or vpcCIDR itself when
// it is already /24 or smaller), used to derive the default node subnet.
func firstSlash24(vpcCIDR string) string {
	if vpcCIDR == "" {
		vpcCIDR = defaultVPCCIDR
	}
	p, err := netip.ParsePrefix(vpcCIDR)
	if err != nil {
		return vpcCIDR
	}
	p = p.Masked()
	if p.Bits() >= 24 {
		return p.String()
	}
	return netip.PrefixFrom(p.Addr(), 24).Masked().String()
}

// gatewayIP derives the subnet gateway IP (network address + 1) from a CIDR.
func gatewayIP(cidr string) string {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return ""
	}
	next := p.Masked().Addr().Next()
	return next.String()
}

// natSpecEnum maps the spec string ("1".."4") to the SDK enum.
func natSpecEnum(spec string) natmodel.CreateNatGatewayOptionSpec {
	switch spec {
	case "2":
		return natmodel.GetCreateNatGatewayOptionSpecEnum().E_2
	case "3":
		return natmodel.GetCreateNatGatewayOptionSpecEnum().E_3
	case "4":
		return natmodel.GetCreateNatGatewayOptionSpecEnum().E_4
	default:
		return natmodel.GetCreateNatGatewayOptionSpecEnum().E_1
	}
}

// joinErrors aggregates errors without importing kerrors (CAPA uses
// kerrors.NewAggregate; errors.Join is the stdlib equivalent and preserves
// individual messages).
func joinErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return stderrors.Join(errs...)
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func int32Ptr(i int32) *int32 { return &i }
