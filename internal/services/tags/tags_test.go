/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package tags

import (
	"strings"
	"testing"
)

// TestOwnedTagKey verifies key construction and the CCE tag-key constraints
// (official: no "/", charset [a-zA-Z0-9_.:=+-@ and space], max 128, not
// starting with "_sys_"). The original slash-form key is invalid for CCE.
func TestOwnedTagKey(t *testing.T) {
	if got := OwnedTagKey("my-cluster"); got != "cluster-api-provider-cce.cluster.my-cluster" {
		t.Errorf("OwnedTagKey(my-cluster) = %q, want %q", got, "cluster-api-provider-cce.cluster.my-cluster")
	}
	for _, name := range []string{"cce-e2e-demo", "my-cluster", "a_very_long_cluster_name_with-many_chars1234567890"} {
		key := OwnedTagKey(name)
		if strings.Contains(key, "/") {
			t.Errorf("OwnedTagKey(%q) = %q: must not contain '/', CCE tag keys reject it", name, key)
		}
		if len(key) > 128 {
			t.Errorf("OwnedTagKey(%q) = %q: exceeds 128-char CCE tag key limit", name, key)
		}
		if strings.HasPrefix(key, "_sys_") {
			t.Errorf("OwnedTagKey(%q) = %q: must not start with _sys_", name, key)
		}
		for _, r := range key {
			if !(r == '.' || r == '-' || r == '_' || r == ':' || r == '=' || r == '+' || r == '@' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				t.Errorf("OwnedTagKey(%q) = %q: contains invalid character %q", name, key, r)
				break
			}
		}
	}
}

// TestIsOwned verifies the owned-tag adoption marker detection.
func TestIsOwned(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want bool
	}{
		{name: "owned", tags: map[string]string{"cluster-api-provider-cce.cluster.foo": "owned"}, want: true},
		{name: "shared value", tags: map[string]string{"cluster-api-provider-cce.cluster.foo": "shared"}, want: false},
		{name: "unrelated", tags: map[string]string{"foo": "owned"}, want: false},
		{name: "wrong cluster", tags: map[string]string{"cluster-api-provider-cce.cluster.bar": "owned"}, want: false},
		{name: "empty", tags: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsOwned(tc.tags, "foo"); got != tc.want {
				t.Errorf("IsOwned(%v, foo) = %v, want %v", tc.tags, got, tc.want)
			}
		})
	}
}

// TestOwnedClusterName verifies the owned-tag -> cluster-name extraction.
func TestOwnedClusterName(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want string
	}{
		{name: "owned tag", tags: map[string]string{"cluster-api-provider-cce.cluster.foo": "owned"}, want: "foo"},
		{name: "non-owned value", tags: map[string]string{"cluster-api-provider-cce.cluster.foo": "shared"}, want: ""},
		{name: "unrelated tag", tags: map[string]string{"foo": "bar"}, want: ""},
		{name: "owned after unrelated", tags: map[string]string{"env": "prod", "cluster-api-provider-cce.cluster.bar": "owned"}, want: "bar"},
		{name: "empty", tags: nil, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OwnedClusterName(tc.tags); got != tc.want {
				t.Errorf("OwnedClusterName(%v) = %q, want %q", tc.tags, got, tc.want)
			}
		})
	}
}
