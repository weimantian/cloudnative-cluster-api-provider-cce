/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package common

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestTagsValidateValid(t *testing.T) {
	// A realistic user tag set: env/owner/cost keys pass, incl. CJK.
	tags := Tags{
		"env":         "prod",
		"owner":       "platform",
		"cost-center": "cc-42",
		"团队":          "基础设施",
	}
	if errs := tags.Validate(field.NewPath("spec", "additionalTags")); len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestTagsValidateEmptyValueAllowed(t *testing.T) {
	// Official CCE constraint: a value may be empty, but the entry must exist.
	if errs := (Tags{"empty": ""}).Validate(field.NewPath("spec", "additionalTags")); len(errs) != 0 {
		t.Fatalf("empty value must be allowed, got %v", errs)
	}
}

func TestTagsValidateSlashAllowedInValue(t *testing.T) {
	// The value charset is wider: '/' is allowed in values (not in keys).
	if errs := (Tags{"k": "a/b"}).Validate(field.NewPath("spec", "additionalTags")); len(errs) != 0 {
		t.Fatalf("'/' in a value must be allowed, got %v", errs)
	}
}

func TestTagsValidateRejects(t *testing.T) {
	longKey := Tags{}
	longKey[strings.Repeat("k", 129)] = "v"
	longValue := Tags{}
	longValue["k"] = strings.Repeat("v", 256)
	tooMany := Tags{}
	for i := 0; i <= MaxAdditionalTags; i++ {
		tooMany["key"+string(rune('a'+i%26))] = "v"
	}

	cases := []struct {
		name string
		tags Tags
		want string // substring expected in at least one error
	}{
		{"empty key", Tags{"": "v"}, "empty"},
		{"slash in key", Tags{"a/b": "v"}, "no '/"},
		{"sys prefix", Tags{"_sys_enterprise_project_id": "v"}, "_sys_"},
		{"leading space in key", Tags{" key": "v"}, "leading or trailing spaces"},
		{"trailing space in key", Tags{"key ": "v"}, "leading or trailing spaces"},
		{"invalid key charset", Tags{"a*key": "v"}, "may only contain"},
		{"key too long", longKey, "longer than 128"},
		{"value too long", longValue, "longer than 255"},
		{"too many tags", tooMany, "at most 18"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.tags.Validate(field.NewPath("spec", "additionalTags"))
			if len(errs) == 0 {
				t.Fatalf("expected validation errors for %q", tc.name)
			}
			joined := errs.ToAggregate().Error()
			if !strings.Contains(joined, tc.want) {
				t.Errorf("expected error containing %q, got: %v", tc.want, errs)
			}
		})
	}
}

// quota builds n valid, distinct tag keys.
func quota(n int) Tags {
	tags := Tags{}
	for i := 0; i < n; i++ {
		tags["key"+string(rune('a'+i%26))] = "v"
	}
	return tags
}

// TestTagsValidateMaxAdditionalTagsBoundary locks the tag budget on BOTH
// resource shapes at their own boundary: the cluster path (Validate) accepts
// exactly MaxAdditionalTags (18) user tags, while the node-pool path
// (ValidateNodePool) accepts only MaxNodePoolAdditionalTags (6) — the lower
// node-pool userTags cap minus the two provider-owned tags. The two caps are
// distinct, so each must be exercised through its own validator.
func TestTagsValidateMaxAdditionalTagsBoundary(t *testing.T) {
	// The derived node-pool cap must stay consistent with the platform limit:
	// MaxNodePoolTags (8) minus the two provider tags = 6.
	if MaxNodePoolAdditionalTags != MaxNodePoolTags-2 {
		t.Fatalf("MaxNodePoolAdditionalTags (%d) must equal MaxNodePoolTags-2 (%d)", MaxNodePoolAdditionalTags, MaxNodePoolTags-2)
	}
	if MaxNodePoolAdditionalTags != 6 {
		t.Fatalf("MaxNodePoolAdditionalTags must be 6, got %d", MaxNodePoolAdditionalTags)
	}

	t.Run("cluster", func(t *testing.T) {
		if errs := quota(MaxAdditionalTags).Validate(field.NewPath("spec", "additionalTags")); len(errs) != 0 {
			t.Fatalf("expected exactly %d cluster tags to be accepted, got %v", MaxAdditionalTags, errs)
		}
		errs := quota(MaxAdditionalTags + 1).Validate(field.NewPath("spec", "additionalTags"))
		if len(errs) == 0 {
			t.Fatalf("expected %d cluster tags to be rejected", MaxAdditionalTags+1)
		}
		if got := errs.ToAggregate().Error(); !strings.Contains(got, "at most") {
			t.Errorf("expected a too-many error, got %v", errs)
		}
	})

	t.Run("node-pool", func(t *testing.T) {
		if errs := quota(MaxNodePoolAdditionalTags).ValidateNodePool(field.NewPath("spec", "additionalTags")); len(errs) != 0 {
			t.Fatalf("expected exactly %d node-pool tags to be accepted, got %v", MaxNodePoolAdditionalTags, errs)
		}
		errs := quota(MaxNodePoolAdditionalTags + 1).ValidateNodePool(field.NewPath("spec", "additionalTags"))
		if len(errs) == 0 {
			t.Fatalf("expected %d node-pool tags to be rejected", MaxNodePoolAdditionalTags+1)
		}
		if got := errs.ToAggregate().Error(); !strings.Contains(got, "at most") {
			t.Errorf("expected a too-many error, got %v", errs)
		}
	})
}

// TestTagsValidateNodePoolReservedPrefixes locks the node-pool UserTag
// restriction: keys starting with "CCE-" or "__type_baremetal" are reserved by
// CCE for node pools, while the cluster ResourceTag path accepts them.
func TestTagsValidateNodePoolReservedPrefixes(t *testing.T) {
	for _, key := range []string{"CCE-owner", "__type_baremetal_x"} {
		t.Run("node-pool rejects/"+key, func(t *testing.T) {
			errs := (Tags{key: "v"}).ValidateNodePool(field.NewPath("spec", "additionalTags"))
			if len(errs) == 0 {
				t.Fatalf("expected %q to be rejected on the node-pool path", key)
			}
		})
		t.Run("cluster accepts/"+key, func(t *testing.T) {
			if errs := (Tags{key: "v"}).Validate(field.NewPath("spec", "additionalTags")); len(errs) != 0 {
				t.Errorf("expected %q to be accepted on the cluster path, got %v", key, errs)
			}
		})
	}
}

// TestValidateMergedTagsCap locks the post-merge node-pool tag cap: the merge
// of control-plane and pool tags is limited to MaxNodePoolAdditionalTags (the
// node-pool userTags limit minus the two provider-owned tags).
func TestValidateMergedTagsCap(t *testing.T) {
	// One over the cap is rejected, naming the limit.
	over := ValidateMergedTags(quota(MaxNodePoolAdditionalTags+1), field.NewPath("spec", "additionalTags"))
	if len(over) == 0 {
		t.Fatalf("expected a merged set of %d tags to be rejected", MaxNodePoolAdditionalTags+1)
	}
	if got := over.ToAggregate().Error(); !strings.Contains(got, "at most") {
		t.Errorf("expected the error to name the limit, got %v", over)
	}

	// A reserved prefix inherited from the control plane is also rejected on
	// the effective node-pool set.
	if errs := ValidateMergedTags(Tags{"CCE-owner": "v"}, field.NewPath("spec", "additionalTags")); len(errs) == 0 {
		t.Error("expected a merged set carrying a reserved prefix to be rejected")
	}

	// Exactly at the cap is accepted (boundary).
	if errs := ValidateMergedTags(quota(MaxNodePoolAdditionalTags), field.NewPath("spec", "additionalTags")); len(errs) != 0 {
		t.Errorf("expected a merged set of exactly %d tags to be accepted, got %v", MaxNodePoolAdditionalTags, errs)
	}
}
