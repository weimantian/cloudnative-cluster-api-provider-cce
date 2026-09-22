/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

func TestSplitEndpointURL(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantHost string
		wantPort int32
	}{
		{name: "ipv4 with port", raw: "https://10.0.0.10:5443", wantHost: "10.0.0.10", wantPort: 5443},
		{name: "hostname with port", raw: "https://example.com:8080", wantHost: "example.com", wantPort: 8080},
		{name: "no port", raw: "https://10.0.0.10", wantHost: "10.0.0.10", wantPort: 0},
		{name: "ipv6 literal", raw: "https://[fe80::1]:6443", wantHost: "fe80::1", wantPort: 6443},
		{name: "non-numeric port", raw: "https://10.0.0.10:notaport", wantHost: "", wantPort: 0},
		{name: "empty", raw: "", wantHost: "", wantPort: 0},
		{name: "scheme only", raw: "https://", wantHost: "", wantPort: 0},
		{name: "missing scheme is invalid", raw: "10.0.0.10:5443", wantHost: "", wantPort: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port := splitEndpointURL(tc.raw)
			if host != tc.wantHost || port != tc.wantPort {
				t.Errorf("splitEndpointURL(%q) = (%q, %d), want (%q, %d)", tc.raw, host, port, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func TestMajorMinor(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "v1.35.0", want: "v1.35"},
		{in: "v1.35.5", want: "v1.35"},
		{in: "v1.35", want: "v1.35"},
		{in: "v1", want: "v1"},
		{in: "", want: ""},
		{in: "v1.35.0-r2", want: "v1.35"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := majorMinor(tc.in); got != tc.want {
				t.Errorf("majorMinor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSameMajorMinor(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{a: "v1.35.0", b: "v1.35.5", want: true},
		{a: "v1.35.0", b: "v1.34.0", want: false},
		{a: "v1.35", b: "v1.35.5", want: true},
		{a: "", b: "", want: true},
		{a: "v1.35.0", b: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			if got := sameMajorMinor(tc.a, tc.b); got != tc.want {
				t.Errorf("sameMajorMinor(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestContainsVersion(t *testing.T) {
	cases := []struct {
		name      string
		targets   []string
		requested string
		want      bool
	}{
		{name: "exact match", targets: []string{"v1.34.8-r2"}, requested: "v1.34.8-r2", want: true},
		{name: "prefix match", targets: []string{"v1.34.8-r2"}, requested: "v1.34", want: true},
		{name: "absent", targets: []string{"v1.34.8-r2"}, requested: "v1.35", want: false},
		{name: "empty targets", targets: nil, requested: "v1.34", want: false},
		{name: "shorter prefix not match", targets: []string{"v1.34.8-r2"}, requested: "v1.3", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsVersion(tc.targets, tc.requested); got != tc.want {
				t.Errorf("containsVersion(%v, %q) = %v, want %v", tc.targets, tc.requested, got, tc.want)
			}
		})
	}
}

func TestEffectiveVPCID(t *testing.T) {
	cases := []struct {
		name string
		spec infrav1beta2.CCECluster
		want string
	}{
		{name: "byo id", spec: infrav1beta2.CCECluster{Spec: infrav1beta2.CCEClusterSpec{Network: common.NetworkSpec{VPC: common.VPC{ID: "vpc-1"}}}}, want: "vpc-1"},
		{name: "managed resource id", spec: infrav1beta2.CCECluster{Spec: infrav1beta2.CCEClusterSpec{Network: common.NetworkSpec{VPC: common.VPC{ResourceID: "vpc-res"}}}}, want: "vpc-res"},
		{name: "id wins over resource", spec: infrav1beta2.CCECluster{Spec: infrav1beta2.CCEClusterSpec{Network: common.NetworkSpec{VPC: common.VPC{ID: "vpc-1", ResourceID: "vpc-res"}}}}, want: "vpc-1"},
		{name: "empty", spec: infrav1beta2.CCECluster{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveVPCID(&tc.spec); got != tc.want {
				t.Errorf("effectiveVPCID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubnetIDs(t *testing.T) {
	cases := []struct {
		name string
		spec infrav1beta2.CCECluster
		want []string
	}{
		{
			name: "byo node subnets exclude eni",
			spec: infrav1beta2.CCECluster{Spec: infrav1beta2.CCEClusterSpec{Network: common.NetworkSpec{Subnets: []common.Subnet{
				{ID: "sub-1", Type: common.SubnetTypeNode},
				{ID: "sub-eni", Type: common.SubnetTypeENI},
				{ID: "sub-2"},
			}}}},
			want: []string{"sub-1", "sub-2"},
		},
		{
			name: "managed resource id fallback",
			spec: infrav1beta2.CCECluster{Spec: infrav1beta2.CCEClusterSpec{Network: common.NetworkSpec{Subnets: []common.Subnet{
				{ResourceID: "sub-res", Type: common.SubnetTypeNode},
			}}}},
			want: []string{"sub-res"},
		},
		{
			name: "id wins over resource id",
			spec: infrav1beta2.CCECluster{Spec: infrav1beta2.CCEClusterSpec{Network: common.NetworkSpec{Subnets: []common.Subnet{
				{ID: "sub-1", ResourceID: "sub-res"},
			}}}},
			want: []string{"sub-1"},
		},
		{name: "empty", spec: infrav1beta2.CCECluster{}, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subnetIDs(&tc.spec)
			if len(got) != len(tc.want) {
				t.Fatalf("subnetIDs() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("subnetIDs()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestToProviderAutoscaling(t *testing.T) {
	in := infrav1beta2.AutoscalingSpec{Enable: true, MinNodeCount: 2, MaxNodeCount: 10}
	got := toProviderAutoscaling(in)
	if got == nil {
		t.Fatal("toProviderAutoscaling() returned nil")
	}
	if got.Enable != true || got.MinNodeCount != 2 || got.MaxNodeCount != 10 {
		t.Errorf("toProviderAutoscaling() = %+v, want mirrored fields", got)
	}
}

func TestToProviderAutoscalingDisabled(t *testing.T) {
	got := toProviderAutoscaling(infrav1beta2.AutoscalingSpec{})
	if got == nil || got.Enable {
		t.Errorf("toProviderAutoscaling(empty) = %+v, want disabled non-nil", got)
	}
}

func TestCredentialsSecretToControlPlane(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := controlplanev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	cpA := &controlplanev1beta2.CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cp-a", Namespace: "default",
			Labels: map[string]string{clusterv1.ClusterNameLabel: "foo"},
		},
	}
	cpOther := &controlplanev1beta2.CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cp-other", Namespace: "default",
			Labels: map[string]string{clusterv1.ClusterNameLabel: "bar"},
		},
	}

	r := &CCEManagedControlPlaneReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cpA, cpOther).Build(),
	}

	reqs := r.credentialsSecretToControlPlane(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "foo-credentials", Namespace: "default"},
	})
	if len(reqs) != 1 || reqs[0].Name != "cp-a" {
		t.Errorf("credentialsSecretToControlPlane(foo-credentials) = %v, want [cp-a]", reqs)
	}

	if reqs := r.credentialsSecretToControlPlane(context.Background(), &corev1.ConfigMap{}); len(reqs) != 0 {
		t.Errorf("expected no requests for non-secret, got %v", reqs)
	}

	if reqs := r.credentialsSecretToControlPlane(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"},
	}); len(reqs) != 0 {
		t.Errorf("expected no requests for non-suffixed secret, got %v", reqs)
	}
}
