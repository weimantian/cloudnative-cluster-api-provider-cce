/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package cce

import (
	"errors"
	"strings"
	"testing"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
)

func TestNewClientCaches(t *testing.T) {
	// NewClient resolves the project ID via a network call on the first
	// build, so a cache hit must short-circuit BEFORE that (no credentials
	// validation / no network). Pre-populate the cache and assert the
	// cached pointer is returned unchanged.
	fake := &Client{}
	clientCache.Store("cn-north-4\x00test-ak-cache\x00test-sk-cache", fake)
	c, err := NewClient("cn-north-4", "test-ak-cache", "test-sk-cache")
	if err != nil {
		t.Fatalf("NewClient (cached) failed: %v", err)
	}
	if c != fake {
		t.Error("expected the cached client pointer for a cache hit")
	}
}

func TestParseTaint(t *testing.T) {
	tests := []struct {
		in         string
		wantKey    string
		wantValue  string
		wantEffect string
	}{
		{in: "dedicated=worker:NoSchedule", wantKey: "dedicated", wantValue: "worker", wantEffect: "NoSchedule"},
		{in: "spot=true:PreferNoSchedule", wantKey: "spot", wantValue: "true", wantEffect: "PreferNoSchedule"},
		{in: "gpu:NoExecute", wantKey: "gpu", wantValue: "", wantEffect: "NoExecute"},
		{in: "plain", wantKey: "plain", wantValue: "", wantEffect: "NoSchedule"},
	}
	for _, tt := range tests {
		key, value, effect := parseTaint(tt.in)
		if key != tt.wantKey || value != tt.wantValue || effect != tt.wantEffect {
			t.Errorf("parseTaint(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tt.in, key, value, effect, tt.wantKey, tt.wantValue, tt.wantEffect)
		}
	}
}

func TestTaintEffectMapping(t *testing.T) {
	noSchedule, err := taintEffect("NoSchedule")
	if err != nil || noSchedule.Value() != "NoSchedule" {
		t.Errorf("taintEffect(NoSchedule) = (%q,%v), want NoSchedule", noSchedule.Value(), err)
	}
	prefer, err := taintEffect("PreferNoSchedule")
	if err != nil || prefer.Value() != "PreferNoSchedule" {
		t.Errorf("taintEffect(PreferNoSchedule) = (%q,%v), want PreferNoSchedule", prefer.Value(), err)
	}
	if _, err := taintEffect("NoScheduel"); err == nil {
		t.Error("expected error for unknown effect")
	}
}

func TestAssembleKubeconfig(t *testing.T) {
	kind := "Config"
	name := "internalCluster"
	server := "https://10.0.0.10:5443"
	ca := "QUJD"
	cert := "Y2xpZW50LWNlcnQ="
	key := "Y2xpZW50LWtleQ=="
	userName := "internal"
	ctxName := "internal"
	current := "internal"
	resp := &model.CreateKubernetesClusterCertResponse{
		Kind: &kind,
		Clusters: &[]model.Clusters{{
			Name: &name,
			Cluster: &model.ClusterCert{
				Server:                   &server,
				CertificateAuthorityData: &ca,
			},
		}},
		Users: &[]model.Users{{
			Name: &userName,
			User: &model.User{
				ClientCertificateData: &cert,
				ClientKeyData:         &key,
			},
		}},
		Contexts: &[]model.Contexts{{
			Name: &ctxName,
			Context: &model.Context{
				Cluster: &name,
				User:    &userName,
			},
		}},
		CurrentContext: &current,
	}

	out, err := assembleKubeconfig(resp)
	if err != nil {
		t.Fatalf("assembleKubeconfig failed: %v", err)
	}
	cfg, err := clientcmd.Load([]byte(out))
	if err != nil {
		t.Fatalf("serialized kubeconfig unparseable: %v", err)
	}
	if cfg.CurrentContext != "internal" {
		t.Errorf("current-context = %q, want internal", cfg.CurrentContext)
	}
	if c := cfg.Clusters["internalCluster"]; c == nil || c.Server != server || string(c.CertificateAuthorityData) != "ABC" {
		t.Errorf("cluster entry mismatch: %+v", c)
	}
	if u := cfg.AuthInfos["internal"]; u == nil || string(u.ClientCertificateData) != "client-cert" || string(u.ClientKeyData) != "client-key" {
		t.Errorf("auth entry mismatch: %+v", u)
	}
	if ctx := cfg.Contexts["internal"]; ctx == nil || ctx.Cluster != "internalCluster" || ctx.AuthInfo != "internal" {
		t.Errorf("context entry mismatch: %+v", ctx)
	}

	// Nil response / empty clusters must error.
	if _, err := assembleKubeconfig(nil); err == nil {
		t.Error("expected error for nil response")
	}
	empty := &model.CreateKubernetesClusterCertResponse{Clusters: &[]model.Clusters{}}
	if _, err := assembleKubeconfig(empty); err == nil {
		t.Error("expected error for empty clusters")
	}
}

func TestAssembleKubeconfigInsecureSkipTLSVerify(t *testing.T) {
	kind := "Config"
	userName := "external"
	ctxName := "external"
	current := "external"

	cases := []struct {
		name     string
		insecure *bool
		want     bool
	}{
		{name: "externalCluster", insecure: boolPtr(true), want: true},
		{name: "externalClusterTLSVerify", insecure: boolPtr(false), want: false},
		{name: "internalCluster", insecure: nil, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clusterName := tc.name
			server := "https://120.46.211.3:5443"
			cert := "Y2xpZW50LWNlcnQ="
			key := "Y2xpZW50LWtleQ=="
			resp := &model.CreateKubernetesClusterCertResponse{
				Kind: &kind,
				Clusters: &[]model.Clusters{{
					Name: &clusterName,
					Cluster: &model.ClusterCert{
						Server:                &server,
						InsecureSkipTlsVerify: tc.insecure,
					},
				}},
				Users: &[]model.Users{{
					Name: &userName,
					User: &model.User{
						ClientCertificateData: &cert,
						ClientKeyData:         &key,
					},
				}},
				Contexts: &[]model.Contexts{{
					Name: &ctxName,
					Context: &model.Context{
						Cluster: &clusterName,
						User:    &userName,
					},
				}},
				CurrentContext: &current,
			}

			out, err := assembleKubeconfig(resp)
			if err != nil {
				t.Fatalf("assembleKubeconfig failed: %v", err)
			}
			cfg, err := clientcmd.Load([]byte(out))
			if err != nil {
				t.Fatalf("serialized kubeconfig unparseable: %v", err)
			}
			c := cfg.Clusters[tc.name]
			if c == nil {
				t.Fatalf("cluster %q missing", tc.name)
			}
			if c.InsecureSkipTLSVerify != tc.want {
				t.Errorf("InsecureSkipTLSVerify = %v, want %v", c.InsecureSkipTLSVerify, tc.want)
			}
		})
	}
}

func TestUpgradeNodePoolBounds(t *testing.T) {
	// Bounds are validated before any SDK call, so a nil SDK client is safe
	// for the invalid range (which returns before touching the SDK).
	c := &Client{}
	for _, mu := range []int32{-1, 0, 21, 100} {
		err := c.UpgradeNodePool(t.Context(), "cluster", "pool", mu)
		if err == nil {
			t.Errorf("expected error for maxUnavailable=%d", mu)
		} else if !strings.Contains(err.Error(), "maxUnavailable must be in [1,20]") {
			t.Errorf("maxUnavailable=%d: expected bounds error, got %q", mu, err.Error())
		}
	}
}

func TestLogConfigTypeMapping(t *testing.T) {
	cases := map[string]string{
		"control":      "control",
		"audit":        "audit",
		"system-addon": "system-addon",
		"":             "control", // default
		"unknown":      "control", // fallback to control
	}
	for in, want := range cases {
		if got := logConfigType(in); got == nil || got.Value() != want {
			t.Errorf("logConfigType(%q) = %v, want %q", in, got, want)
		}
	}
}

func TestUpdateClusterLogConfigBounds(t *testing.T) {
	// TTL bounds are validated before any SDK call, so a nil SDK client is
	// safe for the invalid range.
	c := &Client{}
	for _, ttl := range []int32{-1, 31} {
		err := c.UpdateClusterLogConfig(t.Context(), "cluster", ttl, nil)
		if err == nil || !strings.Contains(err.Error(), "ttlInDays must be in [0,30]") {
			t.Errorf("ttl=%d: expected bounds error, got %v", ttl, err)
		}
	}
}

func TestOwnedTagKeyCCEConstraints(t *testing.T) {
	// The owned tag key must satisfy the CCE ResourceTag key constraints
	// (official: no "/", charset [a-zA-Z0-9_.:=+-@ and space], max 128, not
	// starting with "_sys_"). The original CAPA-style slash key is invalid for
	// CCE (verified live: CCE_CM.0004 "Tag's parameters is invalid").
	for _, name := range []string{"cce-e2e-demo", "my-cluster", "a_very_long_cluster_name_with-many_chars1234567890"} {
		key := ownedTagKey(name)
		if strings.Contains(key, "/") {
			t.Errorf("ownedTagKey(%q) = %q: must not contain '/', CCE tag keys reject it", name, key)
		}
		if len(key) > 128 {
			t.Errorf("ownedTagKey(%q) = %q: exceeds 128-char CCE tag key limit", name, key)
		}
		if strings.HasPrefix(key, "_sys_") {
			t.Errorf("ownedTagKey(%q) = %q: must not start with _sys_", name, key)
		}
		for _, r := range key {
			if !(r == '.' || r == '-' || r == '_' || r == ':' || r == '=' || r == '+' || r == '@' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				t.Errorf("ownedTagKey(%q) = %q: contains invalid character %q", name, key, r)
				break
			}
		}
	}

	// The owned tag must be first and the value "owned".
	tags := toClusterTags("demo", map[string]string{"env": "test"})
	if tags == nil || len(*tags) != 2 {
		t.Fatalf("expected owned + 1 user tag, got %v", tags)
	}
	first := (*tags)[0]
	if first.Key == nil || *first.Key != ownedTagKey("demo") || first.Value == nil || *first.Value != "owned" {
		t.Errorf("unexpected owned tag: key=%v value=%v", first.Key, first.Value)
	}
}

func TestDecodeCertData(t *testing.T) {
	// Real CCE response returns base64-of-PEM; verify single decoding and the
	// raw fallback for non-base64 input (e.g. plain PEM).
	if got := decodeCertData("QUJD"); string(got) != "ABC" {
		t.Errorf("decodeCertData(QUJD) = %q, want ABC", got)
	}
	pem := "-----BEGIN CERTIFICATE-----\nXYZ\n-----END CERTIFICATE-----\n"
	if got := decodeCertData(pem); string(got) != pem {
		t.Errorf("decodeCertData fallback = %q, want raw PEM", got)
	}
}

// TestPaginateAllSinglePage covers the common case where the API returns
// fewer items than pageSize (one call, no marker round-trip).
func TestPaginateAllSinglePage(t *testing.T) {
	calls := 0
	got, err := paginateAll(1000, func(_ *string) ([]int, *string, error) {
		calls++
		return []int{1, 2, 3}, stringPtr("3"), nil
	})
	if err != nil {
		t.Fatalf("paginateAll: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("unexpected result: %v", got)
	}
}

// TestPaginateAllMultiPage simulates the Huawei Cloud v2 marker pattern
// (no next-marker field, page size signals end of list).
func TestPaginateAllMultiPage(t *testing.T) {
	page1 := make([]int, 1000)
	for i := range page1 {
		page1[i] = i
	}
	page2 := []int{1000, 1001}
	calls := 0
	got, err := paginateAll(1000, func(marker *string) ([]int, *string, error) {
		calls++
		if marker == nil {
			return page1, stringPtr("999"), nil
		}
		return page2, stringPtr("1001"), nil
	})
	if err != nil {
		t.Fatalf("paginateAll: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
	if len(got) != 1002 || got[1000] != 1000 || got[1001] != 1001 {
		t.Errorf("expected 1002 items ending [1000, 1001], got len=%d last=%d", len(got), got[len(got)-1])
	}
}

// TestPaginateAllEmptyResult ensures the first call returning 0 items ends
// the loop without further calls.
func TestPaginateAllEmptyResult(t *testing.T) {
	calls := 0
	got, err := paginateAll(1000, func(_ *string) ([]int, *string, error) {
		calls++
		return nil, nil, nil
	})
	if err != nil {
		t.Fatalf("paginateAll: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

// TestPaginateAllErrorStopsIteration ensures a fetch error is returned
// (and the loop stops) without further calls.
func TestPaginateAllErrorStopsIteration(t *testing.T) {
	calls := 0
	sentinel := errors.New("boom")
	got, err := paginateAll(1000, func(_ *string) ([]int, *string, error) {
		calls++
		return nil, nil, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	if got != nil {
		t.Errorf("expected nil result on error, got %v", got)
	}
}
