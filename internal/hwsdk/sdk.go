/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package hwsdk holds the low-level Huawei Cloud SDK list/delete primitives
// shared by the CCE garbage collector (internal/services/cce) and the managed
// network teardown (internal/services/network). Keeping them here gives one
// implementation of each wrapper — and, in particular, one implementation of
// the rule that a NAT gateway's SNAT rules are deleted before the gateway
// itself — so the two callers cannot drift apart.
//
// The primitives return raw SDK errors: each caller keeps its own NotFound
// tolerance and error wrapping.
package hwsdk

import (
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/model"
	natv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2"
	natmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/nat/v2/model"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	vpcmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"

	clouderrors "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/errors"
)

// SnatRule is the subset of a NAT gateway SNAT rule the teardown paths use.
type SnatRule struct {
	ID           string
	NetworkID    string
	FloatingIPID string
}

// ListSnatRules returns the SNAT rules attached to gatewayID.
func ListSnatRules(nat *natv2.NatClient, gatewayID string) ([]SnatRule, error) {
	ids := []string{gatewayID}
	resp, err := nat.ListNatGatewaySnatRules(&natmodel.ListNatGatewaySnatRulesRequest{NatGatewayId: &ids})
	if err != nil {
		return nil, err
	}
	if resp.SnatRules == nil {
		return nil, nil
	}
	out := make([]SnatRule, 0, len(*resp.SnatRules))
	for _, r := range *resp.SnatRules {
		out = append(out, SnatRule{ID: r.Id, NetworkID: r.NetworkId, FloatingIPID: r.FloatingIpId})
	}
	return out, nil
}

// DeleteSnatRule deletes a single SNAT rule.
func DeleteSnatRule(nat *natv2.NatClient, gatewayID, ruleID string) error {
	_, err := nat.DeleteNatGatewaySnatRule(&natmodel.DeleteNatGatewaySnatRuleRequest{
		NatGatewayId: gatewayID,
		SnatRuleId:   ruleID,
	})
	return err
}

// DeleteSnatRules deletes every SNAT rule on gatewayID. A NotFound on the
// listing step counts as "no rules". The listing failure and the first
// rule-deletion failure (with its rule ID) are reported separately so each
// caller keeps its policy: the CCE GC fails on a listing error, while
// managed-network teardown ignores listing errors.
// The rule ID (empty unless a rule deletion failed) comes first; both errors
// are returned last.
func DeleteSnatRules(nat *natv2.NatClient, gatewayID string) (ruleID string, listErr, ruleErr error) {
	rules, err := ListSnatRules(nat, gatewayID)
	if err != nil {
		if clouderrors.IsNotFound(err) {
			return "", nil, nil
		}
		return "", err, nil
	}
	for _, r := range rules {
		if derr := DeleteSnatRule(nat, gatewayID, r.ID); derr != nil && !clouderrors.IsNotFound(derr) {
			return r.ID, nil, derr
		}
	}
	return "", nil, nil
}

// DeleteNatGateway deletes a NAT gateway.
func DeleteNatGateway(nat *natv2.NatClient, gatewayID string) error {
	_, err := nat.DeleteNatGateway(&natmodel.DeleteNatGatewayRequest{NatGatewayId: gatewayID})
	return err
}

// DeleteNatGatewayOrdered is the single implementation of the teardown order
// the platform requires: a gateway's SNAT rules MUST be removed before the
// gateway itself (deleting a gateway that still has SNAT rules is rejected).
// The CCE garbage collector and the managed-network teardown both route
// through it.
//
// deleteRules and deleteGateway carry each caller's NotFound tolerance and
// error wrapping. failFast mirrors the GC behaviour of not attempting the
// gateway delete once the rules delete failed; it is false for the
// managed-network teardown, which runs both steps and aggregates both errors.
func DeleteNatGatewayOrdered(deleteRules, deleteGateway func() error, failFast bool) (rulesErr, gatewayErr error) {
	rulesErr = deleteRules()
	if rulesErr != nil && failFast {
		return rulesErr, nil
	}
	gatewayErr = deleteGateway()
	return rulesErr, gatewayErr
}

// DeletePublicip releases an EIP.
func DeletePublicip(eip *eipv2.EipClient, eipID string) error {
	_, err := eip.DeletePublicip(&eipmodel.DeletePublicipRequest{PublicipId: eipID})
	return err
}

// DeleteVpc deletes a VPC.
func DeleteVpc(vpc *vpcv2.VpcClient, vpcID string) error {
	_, err := vpc.DeleteVpc(&vpcmodel.DeleteVpcRequest{VpcId: vpcID})
	return err
}
