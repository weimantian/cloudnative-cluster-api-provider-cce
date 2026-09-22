/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package common

import (
	"errors"
	"net/netip"
)

// ParseCIDR parses s as an IPv4 or IPv6 CIDR prefix (stdlib net/netip).
// An empty string is reported as an error, so callers can distinguish an
// absent field from a malformed one without a separate empty check.
func ParseCIDR(s string) (netip.Prefix, error) {
	if s == "" {
		return netip.Prefix{}, errors.New("empty CIDR")
	}
	return netip.ParsePrefix(s)
}
