/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package tags is the single source of truth for the provider's ownership tag
// scheme, shared by the cce and network service layers and the controllers so
// any tag-key change is made in exactly one place.
package tags

import "strings"

// OwnedTagPrefix is the provider ownership tag key prefix (owned-tag model).
// NOTE: CCE tag keys cannot contain "/" (official ResourceTag key charset is
// letters/digits/space/_.:=+-@, max 128), so the key uses "." separators
// instead of the slash form. Used for idempotent addressing and future
// external-resource GC.
const OwnedTagPrefix = "cluster-api-provider-cce.cluster"

// OwnedTagKey returns the provider ownership tag key for a cluster
// (OwnedTagPrefix + "." + clusterName).
func OwnedTagKey(clusterName string) string {
	return OwnedTagPrefix + "." + clusterName
}

// IsOwned reports whether tags carry the provider owned tag for clusterName
// (cluster-api-provider-cce.cluster.<name>=owned), the adoption marker.
func IsOwned(tags map[string]string, clusterName string) bool {
	return tags[OwnedTagKey(clusterName)] == "owned"
}

// OwnedClusterName returns the cluster name if tags carry the provider's owned
// tag (cluster-api-provider-cce.cluster.<name>=owned), else "".
func OwnedClusterName(tags map[string]string) string {
	for k, v := range tags {
		if v == "owned" && strings.HasPrefix(k, OwnedTagPrefix+".") {
			return strings.TrimPrefix(k, OwnedTagPrefix+".")
		}
	}
	return ""
}
