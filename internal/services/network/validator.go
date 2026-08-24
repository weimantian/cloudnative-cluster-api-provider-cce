/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package network validates the network a CCE cluster consumes before cluster
// creation. CCE does not create the network — it references existing VPC and
// subnets (official CreateCluster prerequisite). Validation rules verified
// against the official docs (questionnaire Q4/Q7):
//
//   - VPC and requested subnets must exist (subnets must belong to the VPC);
//   - the service CIDR must not overlap the VPC CIDR, any subnet CIDR or the
//     container CIDR (official hard constraint);
//   - eni (Turbo) container subnets count <= 20 (conservative official limit
//     for older versions; newer versions allow up to 100);
//   - "container subnet and node subnet should not share one subnet" is an
//     official *recommendation* (avoid IP exhaustion), surfaced as a warning.
package network

import (
	"context"
	"net/netip"
	"strconv"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	vpcv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/model"
	vpcRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/vpc/v2/region"
	"github.com/pkg/errors"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
)
// maxENISubnets is the conservative official limit for eni container subnets
// (older versions <=20; newer versions <=100 — questionnaire Q4).
const maxENISubnets = 20

// Issue is a single validation finding.
type Issue struct {
	Field   string
	Message string
	// Warning marks a recommendation rather than a hard failure.
	Warning bool
}

// ValidateInput describes the network a CCE cluster references.
type ValidateInput struct {
	VPCID         string
	SubnetIDs     []string
	ContainerMode string // overlay_l2 | vpc-router | eni
	ContainerCIDR string
	ServiceCIDR   string
	ENISubnetIDs  []string
	// CloudSubnetCIDRs is an optional map subnetID -> CIDR; when empty the
	// validator queries the VPC API.
	CloudSubnetCIDRs map[string]string
	VPCCloudCIDR     string
}

// ValidatorInterface is the network validation surface used by controllers;
// *Validator implements it, and tests inject fakes.
type ValidatorInterface interface {
	Validate(ctx context.Context, in ValidateInput) ([]Issue, error)
}

var _ ValidatorInterface = (*Validator)(nil)

// Validator validates the network against the VPC API.
type Validator struct {
	vpc *vpcv2.VpcClient
}

// NewValidator builds a VPC API-backed network validator.
func NewValidator(regionID string, creds *credentials.Credentials) (*Validator, error) {
	region, err := vpcRegion.SafeValueOf(regionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve VPC region %q", regionID)
	}
	builder := basic.NewCredentialsBuilder().WithAk(creds.AccessKey).WithSk(creds.SecretKey)
	if creds.SecurityToken != "" {
		builder = builder.WithSecurityToken(creds.SecurityToken)
	}
	cred, err := builder.SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build VPC credentials")
	}
	hc, err := vpcv2.VpcClientBuilder().
		WithRegion(region).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).
		SafeBuild()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build VPC client")
	}
	return &Validator{vpc: vpcv2.NewVpcClient(hc)}, nil
}

// Validate runs the full set of checks and returns the findings.
func (v *Validator) Validate(ctx context.Context, in ValidateInput) ([]Issue, error) {
	var issues []Issue

	// 0. CIDR format: a malformed CIDR must be reported, not silently treated
	// as "no overlap" (which would pass it through to a cryptic platform error).
	if in.ServiceCIDR != "" && !validCIDR(in.ServiceCIDR) {
		issues = append(issues, Issue{Field: "serviceNetwork.cidr", Message: "invalid CIDR: " + in.ServiceCIDR})
	}
	if in.ContainerCIDR != "" && !validCIDR(in.ContainerCIDR) {
		issues = append(issues, Issue{Field: "containerNetwork.cidr", Message: "invalid CIDR: " + in.ContainerCIDR})
	}
	// eni mode requires at least one ENI subnet (official: eniNetwork.subnets).
	if in.ContainerMode == "eni" && len(in.ENISubnetIDs) == 0 {
		issues = append(issues, Issue{Field: "containerNetwork.eniSubnets",
			Message: "eni mode requires at least one ENI subnet"})
	}

	// Query cloud-side network facts.
	vpcCIDR := in.VPCCloudCIDR
	subnetCIDRs := map[string]string{}
	for k, val := range in.CloudSubnetCIDRs {
		subnetCIDRs[k] = val
	}
	if v != nil && v.vpc != nil {
		cidr, subnets, err := v.fetchNetwork(ctx, in.VPCID)
		if err != nil {
			return nil, err
		}
		if cidr != "" {
			vpcCIDR = cidr
		}
		for id, c := range subnets {
			subnetCIDRs[id] = c
		}
	}

	// 1. VPC exists.
	if vpcCIDR == "" {
		issues = append(issues, Issue{Field: "vpc", Message: "VPC not found or has no CIDR"})
	} else if in.ServiceCIDR != "" && cidrsOverlap(in.ServiceCIDR, vpcCIDR) {
		issues = append(issues, Issue{Field: "serviceNetwork.cidr",
			Message: "service CIDR must not overlap the VPC CIDR (official hard constraint)"})
	}

	// 2. Subnets exist and belong to the VPC.
	for _, id := range in.SubnetIDs {
		if _, ok := subnetCIDRs[id]; !ok {
			issues = append(issues, Issue{Field: "network.subnets", Message: "subnet " + id + " not found in the VPC (CCE.01400002)"})
		}
	}
	for _, id := range in.ENISubnetIDs {
		if _, ok := subnetCIDRs[id]; !ok {
			issues = append(issues, Issue{Field: "containerNetwork.eniSubnets", Message: "eni subnet " + id + " not found in the VPC (CCE.01400002)"})
		}
	}

	// 3. Service CIDR must not overlap any subnet CIDR.
	if in.ServiceCIDR != "" {
		for id, c := range subnetCIDRs {
			if cidrsOverlap(in.ServiceCIDR, c) {
				issues = append(issues, Issue{Field: "serviceNetwork.cidr",
					Message: "service CIDR overlaps subnet " + id + " (" + c + ")"})
			}
		}
	}

	// 4. Container CIDR (vpc-router/overlay) must not overlap service CIDR.
	if in.ContainerMode != "eni" && in.ContainerCIDR != "" && in.ServiceCIDR != "" && cidrsOverlap(in.ContainerCIDR, in.ServiceCIDR) {
		issues = append(issues, Issue{Field: "containerNetwork.cidr",
			Message: "container CIDR overlaps the service CIDR (official hard constraint)"})
	}
	// 4b. Container CIDR must not overlap the VPC CIDR or any subnet CIDR
	// (official hard constraint for vpc-router/overlay modes).
	if in.ContainerMode != "eni" && in.ContainerCIDR != "" {
		if vpcCIDR != "" && cidrsOverlap(in.ContainerCIDR, vpcCIDR) {
			issues = append(issues, Issue{Field: "containerNetwork.cidr",
				Message: "container CIDR overlaps the VPC CIDR (official hard constraint)"})
		}
		for id, c := range subnetCIDRs {
			if cidrsOverlap(in.ContainerCIDR, c) {
				issues = append(issues, Issue{Field: "containerNetwork.cidr",
					Message: "container CIDR overlaps subnet " + id + " (" + c + ")"})
			}
		}
	}

	// 5. eni subnet count limit + recommendation to separate from node subnets.
	if in.ContainerMode == "eni" {
		if len(in.ENISubnetIDs) > maxENISubnets {
			issues = append(issues, Issue{Field: "containerNetwork.eniSubnets",
				Message: "too many eni subnets (max " + strconv.Itoa(maxENISubnets) + " per official limit)"})
		}
		for _, es := range in.ENISubnetIDs {
			for _, ns := range in.SubnetIDs {
				if es == ns {
					issues = append(issues, Issue{Field: "containerNetwork.eniSubnets",
						Message: "eni container subnet " + es + " is also used as a node subnet — official recommendation is to keep them separate (IP exhaustion risk)",
						Warning: true})
				}
			}
		}
	}

	return issues, nil
}

// fetchNetwork queries the VPC CIDR and its subnets (subnetID -> CIDR).
func (v *Validator) fetchNetwork(ctx context.Context, vpcID string) (string, map[string]string, error) {
	out := map[string]string{}
	vpcCIDR := ""
	if vpcID != "" {
		resp, err := v.vpc.ListVpcs(&model.ListVpcsRequest{Id: &vpcID})
		if err != nil {
			return "", nil, errors.Wrapf(err, "ListVpcs %s failed", vpcID)
		}
		if resp.Vpcs != nil && len(*resp.Vpcs) > 0 {
			vpcCIDR = (*resp.Vpcs)[0].Cidr
		}
	}
	subResp, err := v.vpc.ListSubnets(&model.ListSubnetsRequest{VpcId: &vpcID})
	if err != nil {
		return "", nil, errors.Wrapf(err, "ListSubnets of VPC %s failed", vpcID)
	}
	if subResp.Subnets != nil {
		for _, s := range *subResp.Subnets {
			out[s.Id] = s.Cidr
		}
	}
	return vpcCIDR, out, nil
}

// cidrsOverlap reports whether two CIDR prefixes overlap.
func cidrsOverlap(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil {
		return false
	}
	pa = pa.Masked()
	pb = pb.Masked()
	return pa.Contains(pb.Addr()) || pb.Contains(pa.Addr())
}

// validCIDR reports whether s is a parseable CIDR prefix.
func validCIDR(s string) bool {
	_, err := netip.ParsePrefix(s)
	return err == nil
}
