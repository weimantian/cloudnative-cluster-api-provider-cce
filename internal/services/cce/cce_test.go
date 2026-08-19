/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package cce

import (
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
)

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
	key := "Y2xpZW50LWtleQ="
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
	if c := cfg.Clusters["internalCluster"]; c == nil || c.Server != server || string(c.CertificateAuthorityData) != ca {
		t.Errorf("cluster entry mismatch: %+v", c)
	}
	if u := cfg.AuthInfos["internal"]; u == nil || string(u.ClientCertificateData) != cert || string(u.ClientKeyData) != key {
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
