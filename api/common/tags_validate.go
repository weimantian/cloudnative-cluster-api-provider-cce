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
// One resource carries at most MaxResourceTags tags in total. The provider
// owned tag (cluster-api-provider-cce.cluster.<name>=owned) counts toward that
// limit and is always added by the service layer, so user-facing
// AdditionalTags is capped one lower.
const (
	// MaxResourceTags is the maximum number of tags on one Huawei Cloud
	// resource (official limit: 20 per resource).
	MaxResourceTags = 20
	// MaxAdditionalTags reserves one slot for the provider owned tag, which
	// is always added on top of the user tags (the owned tag wins on collision).
	MaxAdditionalTags = MaxResourceTags - 1
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
		if utf8.RuneCountInString(k) > 128 {
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

		if utf8.RuneCountInString(v) > 255 {
			errs = append(errs, field.Invalid(p.Child("value"), v, "tag value cannot be longer than 255 characters"))
		}
		if !tagValueRe.MatchString(v) {
			errs = append(errs, field.Invalid(p.Child("value"), v,
				"tag value may only contain letters, digits, spaces and _ . : / = + - @"))
		}
	}
	return errs
}
