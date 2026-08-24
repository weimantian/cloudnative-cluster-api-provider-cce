/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package iam provides the interface and Huawei Cloud SDK implementation for
// the IAM trust-agency (信任委托) surface consumed by the provider: validate a
// v5 trust-policy document and ensure the agency a CCEClusterRoleIdentity
// references exists, creating it when absent. Controllers depend only on the
// Service interface, so tests can inject fakes (pattern mirrors the cce and
// network packages).
package iam

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"
)

// Service is the IAM agency surface consumed by the provider controllers.
type Service interface {
	// EnsureAgency ensures the trust agency named agencyName exists with the
	// given trust policy, creating it when absent. An existing agency is
	// adopted and never overwritten. It returns nil once the agency exists
	// (either before the call or after a successful create).
	EnsureAgency(ctx context.Context, agencyName, trustPolicy string) error
}

// ValidateTrustPolicy checks that a trust-policy document is valid JSON and
// declares the IAM v5 policy version ("Version": "5.0"). The provider only
// checks the envelope; the statement grammar is validated by the IAM API on
// CreateAgencyV5.
func ValidateTrustPolicy(policy string) error {
	var doc struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal([]byte(policy), &doc); err != nil {
		return errors.Wrap(err, "agencyTrustPolicy is not valid JSON")
	}
	if doc.Version != "5.0" {
		return errors.Errorf("agencyTrustPolicy Version must be %q, got %q", "5.0", doc.Version)
	}
	return nil
}
