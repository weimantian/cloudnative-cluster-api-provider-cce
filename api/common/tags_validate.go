/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package common

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Huawei Cloud resource-tag constraints (official CCE ResourceTag / TMS docs):
//
//	key   — 1..128 characters, no leading/trailing spaces; charset: letters,
//	        digits, spaces and _ . : = + - @ (no '/'); must not start with "_sys_"
//	value — 0..255 characters (empty is allowed, the entry must exist); charset:
//	        letters, digits, spaces and _ . : / = + - @ (allows '/')
//
// One resource carries at most MaxResourceTags tags in total. The service layer
// ALWAYS adds two provider owned tags on top of the user tags: the ownership tag
// (cluster-api-provider-cce.cluster.<name>=owned) and the role tag
// (cluster-api-provider-cce.role), so user-facing AdditionalTags is capped two lower.
const (
	// MaxResourceTags is the maximum number of tags on one Huawei Cloud
	// resource (official limit: 20 per resource).
	MaxResourceTags = 20
	// MaxAdditionalTags reserves two slots for the provider owned tags (the
	// ownership tag and the role tag), which are always added on top of the user
	// tags (the owned tags win on collision). This is the CLUSTER resource cap.
	MaxAdditionalTags = MaxResourceTags - 2
	// MaxNodePoolTags is the maximum number of custom tags CCE accepts on a node
	// pool's nodeTemplate.userTags (official: region dependent, up to 8) — much
	// lower than a cluster resource's 20-tag limit.
	MaxNodePoolTags = 8
	// MaxNodePoolAdditionalTags reserves the two provider tags (ownership + role)
	// that the service always adds to a node pool, so user-facing pool
	// AdditionalTags is capped two lower than the node-pool limit.
	MaxNodePoolAdditionalTags = MaxNodePoolTags - 2
)

// tagKeyRe matches a full valid tag key (no '/', charset _ . : = + - @ and
// any letter/digit/space incl. CJK).
var tagKeyRe = regexp.MustCompile(`^[\p{L}\p{N}\s_.:=+@\-]+$`)

// tagValueRe matches a valid tag value (same charset plus '/').
var tagValueRe = regexp.MustCompile(`^[\p{L}\p{N}\s_.:/=+@\-]*$`)


// Validate checks that t satisfies the Huawei Cloud resource-tag constraints.
// The owned tag is counted toward the per-resource cap only if it is present
// in t (the service always adds it, so an explicit entry here is redundant
// but legal).
func (t Tags) Validate(fldPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if len(t) > MaxAdditionalTags {
		errs = append(errs, field.TooMany(fldPath, len(t), MaxAdditionalTags))
	}

	// Deterministic order for stable error messages.
	for k, v := range t {
		p := fldPath.Key(k)

		if k == "" {
			errs = append(errs, field.Invalid(p, k, "tag key cannot be empty"))
			continue
		}
		if utf8.RuneCountInString(k) > MaxTagKeyLength {
			errs = append(errs, field.Invalid(p, k, "tag key cannot be longer than 128 characters"))
		}
		if k != strings.TrimSpace(k) {
			errs = append(errs, field.Invalid(p, k, "tag key cannot have leading or trailing spaces"))
		}
		if strings.HasPrefix(k, "_sys_") {
			errs = append(errs, field.Invalid(p, k, "tag key cannot start with \"_sys_\" (reserved for system tags)"))
		}
		if !tagKeyRe.MatchString(k) {
			errs = append(errs, field.Invalid(p, k,
				"tag key may only contain letters, digits, spaces and _ . : = + - @ (no '/')"))
		}

		if utf8.RuneCountInString(v) > MaxTagValueLength {
			errs = append(errs, field.Invalid(p.Child("value"), v, "tag value cannot be longer than 255 characters"))
		}
		if !tagValueRe.MatchString(v) {
			errs = append(errs, field.Invalid(p.Child("value"), v,
				"tag value may only contain letters, digits, spaces and _ . : / = + - @"))
		}
	}
	return errs
}

// nodePoolReservedKeyPrefixes lists tag-key prefixes the CCE node-pool UserTag
// model forbids on top of the shared resource-tag rules: an SDK UserTag key
// must not start with "CCE-" or "__type_baremetal". The cluster ResourceTag
// model has no such restriction, so these are enforced ONLY by ValidateNodePool
// (the CCEManagedMachinePool path) and never by Validate (the cluster path).
var nodePoolReservedKeyPrefixes = []string{"CCE-", "__type_baremetal"}

// ValidateNodePool checks that t satisfies the constraints for a node pool's
// user tags (CCE UserTag model): the shared resource-tag rules, the lower
// node-pool tag-count cap, and the node-pool-only reserved key prefixes.
// Cluster additionalTags (the ResourceTag model) must keep using Validate,
// which allows those prefixes and the higher cluster tag cap.
func (t Tags) ValidateNodePool(fldPath *field.Path) field.ErrorList {
	errs := t.Validate(fldPath)

	// A node pool accepts far fewer custom tags than a cluster resource
	// (nodeTemplate.userTags: up to 8, minus the two provider tags).
	if len(t) > MaxNodePoolAdditionalTags {
		errs = append(errs, field.TooMany(fldPath, len(t), MaxNodePoolAdditionalTags))
	}

	for _, prefix := range nodePoolReservedKeyPrefixes {
		for k := range t {
			if strings.HasPrefix(k, prefix) {
				errs = append(errs, field.Invalid(fldPath.Key(k), k,
					"tag key cannot start with \""+prefix+"\" (reserved by CCE for node pools)"))
			}
		}
	}
	return errs
}

// ValidateMergedTags checks the effective tag set a node pool will carry: the
// control plane's additionalTags merged with the pool's additionalTags (the same
// order the node-pool controller merges them). Each side is admitted
// independently, but their union can exceed MaxNodePoolAdditionalTags, and a
// control-plane key may carry a prefix the node-pool UserTag model forbids; the
// platform rejects the node-pool create in both cases. The effective set is
// therefore validated with the node-pool rules at the merge site before the
// create/update (MaxNodePoolAdditionalTags reserves the two tags the service
// always adds: ownership and role).
//
// The node-pool admission webhook cannot run this check: it has no client and
// cannot read the control plane's additionalTags, so enforcement lives in the
// controller.
func ValidateMergedTags(merged Tags, fldPath *field.Path) field.ErrorList {
	return merged.ValidateNodePool(fldPath)
}
