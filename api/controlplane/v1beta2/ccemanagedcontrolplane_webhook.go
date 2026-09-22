/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"
	"regexp"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8sSemver "k8s.io/apimachinery/pkg/util/version"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
)

const (
	// minKubeVersionForIPv6 is the minimum Kubernetes version that supports
	// IPv6 dual-stack (official CCE constraint).
	minKubeVersionForIPv6 = "v1.21.0"
)

// ipv6Enabled reports whether the ipv6enable flag is explicitly true.
func ipv6Enabled(b *bool) bool {
	return b != nil && *b
}

// SetupWebhookWithManager registers the CCEManagedControlPlane webhook.
func (c *CCEManagedControlPlane) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return builder.WebhookManagedBy(mgr, &CCEManagedControlPlane{}).
		WithDefaulter(&CCEManagedControlPlane{}).
		WithValidator(&CCEManagedControlPlane{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-controlplane-cluster-x-k8s-io-v1beta2-ccemanagedcontrolplane,mutating=true,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=create;update,versions=v1beta2,name=mutation.ccemanagedcontrolplane.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-controlplane-cluster-x-k8s-io-v1beta2-ccemanagedcontrolplane,mutating=false,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=ccemanagedcontrolplanes,verbs=create;update,versions=v1beta2,name=validation.ccemanagedcontrolplane.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1

var _ admission.Defaulter[*CCEManagedControlPlane] = &CCEManagedControlPlane{}

// Default implements admission.Defaulter.
func (c *CCEManagedControlPlane) Default(_ context.Context, obj *CCEManagedControlPlane) error {
	applyControlPlaneDefaults(&obj.Spec)
	return nil
}

// applyControlPlaneDefaults fills the spec defaults shared by the
// CCEManagedControlPlane and its ClusterClass template, so the two admission
// paths cannot diverge. Mode is resolved before Category because Category
// follows the network mode: eni = Turbo, vpc-router/overlay_l2 = Standard
// (CCE). Defaulting Turbo regardless of mode produced a CCE_CM.0004
// type/network-mode mismatch (mode=vpc-router + Turbo) and made Standard
// clusters unusable through ClusterClass.
func applyControlPlaneDefaults(spec *CCEManagedControlPlaneSpec) {
	if spec.ContainerNetwork.Mode == "" {
		spec.ContainerNetwork.Mode = common.ModeENI
	}
	if spec.Category == "" {
		if spec.ContainerNetwork.Mode == common.ModeENI {
			spec.Category = "Turbo"
		} else {
			spec.Category = "CCE"
		}
	}
	if spec.Flavor == "" {
		spec.Flavor = common.DefaultFlavor
	}
}

var _ admission.Validator[*CCEManagedControlPlane] = &CCEManagedControlPlane{}

// ValidateCreate implements admission.Validator.
func (c *CCEManagedControlPlane) ValidateCreate(_ context.Context, obj *CCEManagedControlPlane) (admission.Warnings, error) {
	return nil, obj.validate()
}

// ValidateUpdate implements admission.Validator.
func (c *CCEManagedControlPlane) ValidateUpdate(_ context.Context, oldObj, newObj *CCEManagedControlPlane) (admission.Warnings, error) {
	var allErrs field.ErrorList
	// Immutable fields: the CCE cluster network config cannot change after
	// creation (official: container network CIDR/mode are immutable). Accepting
	// a change silently would drift spec from the cloud.
	// Region is on the sibling CCECluster and is not replicated here; the
	// CCECluster webhook enforces region immutability.
	if oldObj.Spec.ClusterName != newObj.Spec.ClusterName {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "clusterName"),
			newObj.Spec.ClusterName, "field is immutable after creation"))
	}
	if oldObj.Spec.ContainerNetwork.CIDR != newObj.Spec.ContainerNetwork.CIDR {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "cidr"),
			newObj.Spec.ContainerNetwork.CIDR, "field is immutable after creation"))
	}
	if oldObj.Spec.ContainerNetwork.Mode != newObj.Spec.ContainerNetwork.Mode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "mode"),
			newObj.Spec.ContainerNetwork.Mode, "field is immutable after creation"))
	}
	if oldObj.Spec.Category != newObj.Spec.Category {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "category"),
			newObj.Spec.Category, "field is immutable after creation"))
	}
	// Encryption mode and authentication mode are immutable (CCE does not
	// support changing them post-create).
	if oldObj.Spec.EncryptionConfig != nil && newObj.Spec.EncryptionConfig != nil &&
		oldObj.Spec.EncryptionConfig.Mode != newObj.Spec.EncryptionConfig.Mode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "encryptionConfig", "mode"),
			newObj.Spec.EncryptionConfig.Mode, "field is immutable after creation"))
	}
	if oldObj.Spec.Authentication != nil && newObj.Spec.Authentication != nil &&
		oldObj.Spec.Authentication.Mode != newObj.Spec.Authentication.Mode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "authentication", "mode"),
			newObj.Spec.Authentication.Mode, "field is immutable after creation"))
	}
	// Version downgrade is rejected (official: in-place upgrades only).
	if oldObj.Spec.Version != "" && newObj.Spec.Version != "" {
		oldV, oldErr := k8sSemver.ParseSemantic(oldObj.Spec.Version)
		newV, newErr := k8sSemver.ParseSemantic(newObj.Spec.Version)
		if oldErr == nil && newErr == nil && newV.LessThan(oldV) {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "version"),
				newObj.Spec.Version, "version cannot be downgraded"))
		}
	}
	// Flavor downgrade is rejected: a CCE cluster's capacity can only be
	// scaled up, never down (the platform cannot reclaim an already-allocated
	// cluster scale). Unknown flavor shapes are left to the platform.
	if oldObj.Spec.Flavor != "" && newObj.Spec.Flavor != "" && oldObj.Spec.Flavor != newObj.Spec.Flavor {
		if oldRank, ok := cceFlavorRank(oldObj.Spec.Flavor); ok {
			if newRank, ok2 := cceFlavorRank(newObj.Spec.Flavor); ok2 && newRank < oldRank {
				allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "flavor"),
					newObj.Spec.Flavor, "flavor cannot be downgraded (cluster capacity can only be scaled up)"))
			}
		}
	}
	// Encryption config cannot be removed once set (etcd encryption is
	// irreversible on CCE).
	if oldObj.Spec.EncryptionConfig != nil && newObj.Spec.EncryptionConfig == nil {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "encryptionConfig"),
			"encryptionConfig cannot be removed once set"))
	}
	// Identity reference cannot be cleared once set.
	if oldObj.Spec.IdentityRef != nil && newObj.Spec.IdentityRef == nil {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "identityRef"),
			"identityRef cannot be removed once set"))
	}
	// IPv6 enablement is immutable (changing the IP family of a live cluster
	// is not supported).
	// DataPlane V2 can only be enabled at cluster creation (the platform does
	// not allow disabling it or opting in later), so it is immutable.
	if ipv6Enabled(oldObj.Spec.EnableDataPlaneV2) != ipv6Enabled(newObj.Spec.EnableDataPlaneV2) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "enableDataPlaneV2"),
			newObj.Spec.EnableDataPlaneV2, "field is immutable after creation"))
	}
	if ipv6Enabled(oldObj.Spec.Ipv6Enable) != ipv6Enabled(newObj.Spec.Ipv6Enable) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "ipv6enable"),
			newObj.Spec.Ipv6Enable, "field is immutable after creation"))
	}
	if err := newObj.validate(); err != nil {
		return nil, err
	}
	if len(allErrs) > 0 {
		return nil, apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
	}
	return nil, nil
}

// ValidateDelete implements admission.Validator.
func (c *CCEManagedControlPlane) ValidateDelete(_ context.Context, _ *CCEManagedControlPlane) (admission.Warnings, error) {
	return nil, nil
}

func (c *CCEManagedControlPlane) validate() error {
	var allErrs field.ErrorList

	if c.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "clusterName"), "clusterName is required"))
	}
	switch c.Spec.Category {
	case "", "CCE", "Turbo":
	default:
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "category"), c.Spec.Category, "must be CCE or Turbo"))
	}
	// eni mode implies Turbo (official SDK comment).
	if c.Spec.ContainerNetwork.Mode == common.ModeENI && c.Spec.Category == "CCE" {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "mode"),
			common.ModeENI, "eni mode requires category Turbo"))
	}
	// vpc-router mode requires category CCE (Turbo only supports eni).
	if c.Spec.ContainerNetwork.Mode == common.ModeVPCRouter && c.Spec.Category == "Turbo" {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "mode"),
			common.ModeVPCRouter, "vpc-router mode requires category CCE"))
	}
	// eni mode requires ENI subnets (official: eniNetwork must set subnets or
	// eniSubnetId — our CRD exposes subnets via eniSubnets).
	if c.Spec.ContainerNetwork.Mode == common.ModeENI && len(c.Spec.ContainerNetwork.ENISubnets) == 0 {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "containerNetwork", "eniSubnets"),
			"eni mode requires at least one ENI subnet (official eniNetwork.subnets)"))
	}
	// DataPlane V2 is a configuration item of the eni component group in
	// spec.configurationsOverride, and the platform supports it for both the
	// eni (Turbo) and vpc-router (Standard) network models — the CCE console
	// emits the same eni.dataplane-v2 override for either. The container
	// tunnel model (overlay_l2) has no such switch.
	if ipv6Enabled(c.Spec.EnableDataPlaneV2) && c.Spec.ContainerNetwork.Mode != common.ModeENI && c.Spec.ContainerNetwork.Mode != common.ModeVPCRouter {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "enableDataPlaneV2"), "true",
			"DataPlane V2 requires containerNetwork.mode=eni (Turbo) or vpc-router (Standard)"))
	}
	// Subscription billing (mode=1) requires periodType/periodNum which the
	// CRD does not expose yet — reject it explicitly instead of letting the
	// create loop fail forever on a missing required field.
	if c.Spec.Billing.Mode == 1 {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "billing", "mode"), "1",
			"subscription billing is not supported yet (periodType/periodNum not exposed)"))
	}
	// authenticating_proxy mode requires the CA + client cert + key.
	if c.Spec.Authentication != nil && c.Spec.Authentication.Mode == "authenticating_proxy" {
		ap := c.Spec.Authentication.AuthenticatingProxy
		if ap == nil || ap.CA == "" || ap.Cert == "" || ap.PrivateKey == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "authentication", "authenticatingProxy"),
				"authenticating_proxy mode requires ca, cert and privateKey"))
		}
	}
	// CIDR format check: ContainerNetwork.CIDR must be a valid IPv4/IPv6 CIDR.
	if c.Spec.ContainerNetwork.CIDR != "" {
		if _, err := common.ParseCIDR(c.Spec.ContainerNetwork.CIDR); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "containerNetwork", "cidr"),
				c.Spec.ContainerNetwork.CIDR, "must be a valid IPv4/IPv6 CIDR (e.g. 10.0.0.0/16)"))
		}
	}
	// Version format check: k8s semver (vMAJOR.MINOR.PATCH with optional -prerelease).
	if c.Spec.Version != "" {
		if _, err := k8sSemver.ParseSemantic(c.Spec.Version); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "version"),
				c.Spec.Version, "must be a semver (e.g. v1.30.1 or v1.30.1-rc.1)"))
		}
	}
	// IPv6 dual-stack requires Kubernetes v1.21.0+ (official CCE constraint).
	if ipv6Enabled(c.Spec.Ipv6Enable) && c.Spec.Version != "" {
		v, err := k8sSemver.ParseSemantic(c.Spec.Version)
		if err == nil {
			minV, _ := k8sSemver.ParseSemantic(minKubeVersionForIPv6)
			if v.LessThan(minV) {
				allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "version"),
					c.Spec.Version, "IPv6 dual-stack requires Kubernetes v1.21.0 or later"))
			}
		}
	}
	// Service network CIDR format.
	if c.Spec.ServiceNetwork.CIDR != "" {
		if _, err := common.ParseCIDR(c.Spec.ServiceNetwork.CIDR); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "serviceNetwork", "cidr"),
				c.Spec.ServiceNetwork.CIDR, "must be a valid IPv4/IPv6 CIDR (e.g. 10.247.0.0/16)"))
		}
	}
	// IPv6 service network CIDR is required when ipv6enable is true.
	if ipv6Enabled(c.Spec.Ipv6Enable) && c.Spec.ServiceNetwork.IPv6CIDR == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "serviceNetwork", "ipv6CIDR"),
			"ipv6CIDR is required when ipv6enable is true"))
	}
	// IPv6 service network CIDR format.
	if c.Spec.ServiceNetwork.IPv6CIDR != "" {
		if _, err := common.ParseCIDR(c.Spec.ServiceNetwork.IPv6CIDR); err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "serviceNetwork", "ipv6CIDR"),
				c.Spec.ServiceNetwork.IPv6CIDR, "must be a valid IPv6 CIDR (e.g. fd00::/112)"))
		}
	}
	// Additional container network CIDRs must be valid and distinct from the
	// primary CIDR (official: container CIDRs must be unique per VPC).
	for i, cidr := range c.Spec.ContainerNetwork.CIDRs {
		p := field.NewPath("spec", "containerNetwork", "cidrs").Index(i)
		if _, err := common.ParseCIDR(cidr); err != nil {
			allErrs = append(allErrs, field.Invalid(p, cidr, "must be a valid IPv4/IPv6 CIDR"))
		}
		if c.Spec.ContainerNetwork.CIDR != "" && cidr == c.Spec.ContainerNetwork.CIDR {
			allErrs = append(allErrs, field.Invalid(p, cidr, "must not be equal to the primary container network CIDR"))
		}
	}
	// Endpoint access whitelist CIDRs must be valid.
	for i, cidr := range c.Spec.EndpointAccess.CIDRs {
		p := field.NewPath("spec", "endpointAccess", "cidrs").Index(i)
		if _, err := common.ParseCIDR(cidr); err != nil {
			allErrs = append(allErrs, field.Invalid(p, cidr, "must be a valid IPv4/IPv6 CIDR"))
		}
	}
	// CCE always exposes a VPC-internal (private) API server endpoint and
	// cannot disable it, so private: false is rejected.
	if !c.Spec.EndpointAccess.Private {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "endpointAccess", "private"),
			"private cannot be disabled (CCE always exposes a VPC-internal endpoint)"))
	}
	// Additional tags follow the Huawei Cloud resource-tag constraints (official
	// CCE ResourceTag limits: key 1-128 no '/' no _sys_, value 0-255, <=19 user
	// tags so the owned tag keeps the total at the official 20-cap).
	allErrs = append(allErrs, c.Spec.AdditionalTags.Validate(field.NewPath("spec", "additionalTags"))...)
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(c.GroupVersionKind().GroupKind(), c.Name, allErrs)
}

// cceFlavorSizes ranks the size segment of a CCE cluster flavor
// (cce.s<N>.<size>). Small→large→xlarge→2xlarge→… ; unknown sizes make the
// flavor unrankable (fail-open, the platform still validates it).
var cceFlavorSizes = map[string]int{
	"small": 1, "medium": 2, "large": 3, "xlarge": 4,
	"2xlarge": 5, "4xlarge": 6, "8xlarge": 7, "16xlarge": 8,
}

// cceFlavorRank parses "cce.s<N>.<size>" into a comparable rank so the webhook
// can reject downgrades. ok=false for any shape it does not understand.
var cceFlavorRe = regexp.MustCompile(`^cce\.s(\d+)\.([a-z0-9]+)$`)

func cceFlavorRank(flavor string) (int, bool) {
	m := cceFlavorRe.FindStringSubmatch(flavor)
	if m == nil {
		return 0, false
	}
	scale, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	size, ok := cceFlavorSizes[m[2]]
	if !ok {
		return 0, false
	}
	return scale*100 + size, true
}
