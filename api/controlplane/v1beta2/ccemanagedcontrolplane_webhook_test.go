/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta2

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// validCP returns a control plane that passes the base validation so tests can
// focus on the field under test.
func validCP() *CCEManagedControlPlane {
	return &CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "cp", Namespace: "default"},
		Spec: CCEManagedControlPlaneSpec{
			ClusterName: "test",
			Category:    "Turbo",
			ContainerNetwork: ContainerNetworkSpec{
				Mode:       "eni",
				CIDR:       "10.0.0.0/16",
				ENISubnets: []string{"subnet-1"},
			},
			Version:        "v1.30.0",
			EndpointAccess: EndpointAccessSpec{Private: true},
		},
	}
}

// TestCCEManagedControlPlaneValidateIPv6 covers the P1-#5 IPv6 additions:
// min Kubernetes version and the required/valid service-network IPv6CIDR.
func TestCCEManagedControlPlaneValidateIPv6(t *testing.T) {
	enable := true
	cases := []struct {
		name     string
		mutate   func(*CCEManagedControlPlane)
		wantFail bool
	}{
		{
			name: "ipv6 with old kubernetes rejected",
			mutate: func(c *CCEManagedControlPlane) {
				c.Spec.Ipv6Enable = &enable
				c.Spec.Version = "v1.20.0"
				c.Spec.ServiceNetwork.IPv6CIDR = "fd00::/112"
			},
			wantFail: true,
		},
		{
			name: "ipv6 with modern kubernetes and ipv6CIDR passes",
			mutate: func(c *CCEManagedControlPlane) {
				c.Spec.Ipv6Enable = &enable
				c.Spec.ServiceNetwork.IPv6CIDR = "fd00::/112"
			},
			wantFail: false,
		},
		{
			name: "ipv6 missing ipv6CIDR rejected",
			mutate: func(c *CCEManagedControlPlane) {
				c.Spec.Ipv6Enable = &enable
			},
			wantFail: true,
		},
		{
			name: "invalid ipv6CIDR rejected",
			mutate: func(c *CCEManagedControlPlane) {
				c.Spec.Ipv6Enable = &enable
				c.Spec.ServiceNetwork.IPv6CIDR = "not-a-cidr"
			},
			wantFail: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCP()
			tc.mutate(c)
			err := c.validate()
			if tc.wantFail && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantFail && err != nil {
				t.Errorf("expected valid, got %v", err)
			}
		})
	}
}

// TestCCEManagedControlPlaneValidateServiceNetworkCIDR covers the service
// network CIDR format check.
func TestCCEManagedControlPlaneValidateServiceNetworkCIDR(t *testing.T) {
	c := validCP()
	c.Spec.ServiceNetwork.CIDR = "10.247.0.0/16"
	if err := c.validate(); err != nil {
		t.Errorf("expected valid service CIDR, got %v", err)
	}

	bad := validCP()
	bad.Spec.ServiceNetwork.CIDR = "999.0.0.0/16"
	if err := bad.validate(); err == nil {
		t.Error("expected invalid service CIDR to be rejected")
	}
}

// TestCCEManagedControlPlaneValidateAdditionalCIDRs covers secondary container
// CIDR format and the "distinct from primary" rule.
func TestCCEManagedControlPlaneValidateAdditionalCIDRs(t *testing.T) {
	ok := validCP()
	ok.Spec.ContainerNetwork.CIDRs = []string{"10.1.0.0/16"}
	if err := ok.validate(); err != nil {
		t.Errorf("expected valid additional CIDR, got %v", err)
	}

	badFormat := validCP()
	badFormat.Spec.ContainerNetwork.CIDRs = []string{"nope"}
	if err := badFormat.validate(); err == nil {
		t.Error("expected malformed additional CIDR to be rejected")
	}

	dupe := validCP()
	dupe.Spec.ContainerNetwork.CIDRs = []string{"10.0.0.0/16"} // == primary
	if err := dupe.validate(); err == nil {
		t.Error("expected additional CIDR equal to primary to be rejected")
	}
}

// TestCCEManagedControlPlaneValidateEndpointAccessCIDRs covers the endpoint
// access whitelist CIDR format check.
func TestCCEManagedControlPlaneValidateEndpointAccessCIDRs(t *testing.T) {
	ok := validCP()
	ok.Spec.EndpointAccess.CIDRs = []string{"0.0.0.0/0"}
	if err := ok.validate(); err != nil {
		t.Errorf("expected valid endpoint CIDR, got %v", err)
	}

	bad := validCP()
	bad.Spec.EndpointAccess.CIDRs = []string{"10.0.0.0"}
	if err := bad.validate(); err == nil {
		t.Error("expected malformed endpoint CIDR to be rejected")
	}
}

// TestCCEManagedControlPlaneValidateEndpointAccessPrivate covers the P2-1
// private field: CCE's private endpoint is always-on, so private: false is
// rejected.
func TestCCEManagedControlPlaneValidateEndpointAccessPrivate(t *testing.T) {
	ok := validCP()
	ok.Spec.EndpointAccess.Private = true
	ok.Spec.EndpointAccess.Public = true
	if err := ok.validate(); err != nil {
		t.Errorf("expected private=true to pass, got %v", err)
	}

	bad := validCP()
	bad.Spec.EndpointAccess.Private = false
	if err := bad.validate(); err == nil {
		t.Error("expected private=false to be rejected")
	}
}

// TestCCEManagedControlPlaneValidateUpdate covers version downgrade and the
// remove-once-set immutability rules added in P1-#5.
func TestCCEManagedControlPlaneValidateUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("version downgrade rejected", func(t *testing.T) {
		old := validCP()
		old.Spec.Version = "v1.30.0"
		new := validCP()
		new.Spec.Version = "v1.29.0"
		if _, err := old.ValidateUpdate(ctx, old, new); err == nil {
			t.Error("expected downgrade to be rejected")
		}
	})

	t.Run("version upgrade accepted", func(t *testing.T) {
		old := validCP()
		old.Spec.Version = "v1.29.0"
		new := validCP()
		new.Spec.Version = "v1.30.0"
		if _, err := old.ValidateUpdate(ctx, old, new); err != nil {
			t.Errorf("expected upgrade to be accepted, got %v", err)
		}
	})

	t.Run("encryptionConfig removal rejected", func(t *testing.T) {
		old := validCP()
		old.Spec.EncryptionConfig = &EncryptionConfigSpec{Mode: "KMS"}
		new := validCP()
		if _, err := old.ValidateUpdate(ctx, old, new); err == nil {
			t.Error("expected encryptionConfig removal to be rejected")
		}
	})

	t.Run("identityRef removal rejected", func(t *testing.T) {
		old := validCP()
		old.Spec.IdentityRef = &corev1.ObjectReference{Name: "id", Namespace: "default"}
		new := validCP()
		if _, err := old.ValidateUpdate(ctx, old, new); err == nil {
			t.Error("expected identityRef removal to be rejected")
		}
	})

	t.Run("ipv6 enablement immutable", func(t *testing.T) {
		enable := true
		old := validCP()
		old.Spec.Ipv6Enable = &enable
		old.Spec.ServiceNetwork.IPv6CIDR = "fd00::/112"
		new := validCP()
		if _, err := old.ValidateUpdate(ctx, old, new); err == nil {
			t.Error("expected ipv6 disable on update to be rejected")
		}
	})
}

func TestControlPlaneFlavorDowngradeRejected(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		old, new string
		wantErr  bool
	}{
		{"upgrade s1->s2", "cce.s1.small", "cce.s2.small", false},
		{"upgrade size", "cce.s2.small", "cce.s2.xlarge", false},
		{"same", "cce.s2.small", "cce.s2.small", false},
		{"downgrade scale", "cce.s2.xlarge", "cce.s1.xlarge", true},
		{"downgrade size", "cce.s2.xlarge", "cce.s2.small", true},
		{"unknown shapes pass", "weird", "other", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := validCP()
			old.Spec.Flavor = tc.old
			nw := validCP()
			nw.Spec.Flavor = tc.new
			_, err := old.ValidateUpdate(ctx, old, nw)
			if (err != nil) != tc.wantErr {
				t.Errorf("flavor %s -> %s: err=%v, wantErr=%v", tc.old, tc.new, err, tc.wantErr)
			}
		})
	}
}

func TestControlPlaneDataPlaneV2(t *testing.T) {
	ctx := context.Background()
	// create-time: supported for eni (Turbo) and vpc-router (Standard) — the
	// CCE console emits the same eni.dataplane-v2 override for either.
	b := true
	c := validCP() // eni defaults via Mode=eni in validCP
	c.Spec.EnableDataPlaneV2 = &b
	if err := c.validate(); err != nil {
		t.Errorf("eni + DPv2 should validate, got %v", err)
	}
	c2 := validCP()
	c2.Spec.ContainerNetwork.Mode = "vpc-router"
	c2.Spec.Category = "CCE"
	c2.Spec.EnableDataPlaneV2 = &b
	if err := c2.validate(); err != nil {
		t.Errorf("vpc-router (Standard) + DPv2 should validate, got %v", err)
	}
	// the container-tunnel model has no DPv2 switch.
	c3 := validCP()
	c3.Spec.ContainerNetwork.Mode = "overlay_l2"
	c3.Spec.Category = "CCE"
	c3.Spec.EnableDataPlaneV2 = &b
	if err := c3.validate(); err == nil {
		t.Error("overlay_l2 + DPv2 must be rejected")
	}
	// immutable after creation.
	old := validCP()
	nw := validCP()
	nw.Spec.EnableDataPlaneV2 = &b
	if _, err := old.ValidateUpdate(ctx, old, nw); err == nil {
		t.Error("enabling DPv2 on an existing cluster must be rejected")
	}
}

// TestCCEManagedControlPlaneDefaultVpcRouter verifies the raw control plane
// path derives category CCE from mode=vpc-router when category is omitted
// (Turbo only supports eni), and that omitting mode still defaults to
// eni + Turbo.
func TestCCEManagedControlPlaneDefaultVpcRouter(t *testing.T) {
	ctx := context.Background()

	vpc := &CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "cp"},
		Spec: CCEManagedControlPlaneSpec{
			ClusterName:      "test",
			ContainerNetwork: ContainerNetworkSpec{Mode: "vpc-router"},
			EndpointAccess:   EndpointAccessSpec{Private: true},
		},
	}
	if err := vpc.Default(ctx, vpc); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if vpc.Spec.Category != "CCE" {
		t.Errorf("expected vpc-router to default category CCE, got %q", vpc.Spec.Category)
	}
	if err := vpc.validate(); err != nil {
		t.Errorf("expected vpc-router control plane to validate, got %v", err)
	}

	empty := &CCEManagedControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "cp2"}}
	if err := empty.Default(ctx, empty); err != nil {
		t.Fatalf("Default returned error: %v", err)
	}
	if empty.Spec.ContainerNetwork.Mode != "eni" || empty.Spec.Category != "Turbo" {
		t.Errorf("expected omitted mode to default eni + Turbo, got mode=%q category=%q",
			empty.Spec.ContainerNetwork.Mode, empty.Spec.Category)
	}
}
