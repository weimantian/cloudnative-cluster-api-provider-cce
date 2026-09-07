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
		{"too many tags", tooMany, "at most 19"},
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
