/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package cce

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
	eipv2 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2"
	eipregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/eip/v2/region"
)

type cceRoute struct {
	method string
	sub    string // substring of "PATH?QUERY"
	status int
	body   string
}

// recordingCCERT serves canned CCE responses and records every request line and
// body, so service methods can be asserted without the network.
type recordingCCERT struct {
	t        *testing.T
	routes   []cceRoute
	requests []string
	bodies   []string
}

func cceTarget(req *http.Request) string {
	if req.URL.RawQuery == "" {
		return req.URL.Path
	}
	return req.URL.Path + "?" + req.URL.RawQuery
}

func (r *recordingCCERT) RoundTrip(req *http.Request) (*http.Response, error) {
	target := cceTarget(req)
	body, _ := io.ReadAll(req.Body)
	r.requests = append(r.requests, req.Method+" "+target)
	r.bodies = append(r.bodies, string(body))
	for _, rt := range r.routes {
		if rt.method == req.Method && strings.Contains(target, rt.sub) {
			status := rt.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(rt.body)),
				Request:    req,
			}, nil
		}
	}
	r.t.Errorf("unexpected request %s %s", req.Method, target)
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error_code":"unexpected","error_msg":"unexpected request"}`)),
		Request:    req,
	}, nil
}

func (r *recordingCCERT) index(method, sub string) int {
	for i, q := range r.requests {
		if strings.HasPrefix(q, method+" ") && strings.Contains(q, sub) {
			return i
		}
	}
	return -1
}

func (r *recordingCCERT) count(method, sub string) int {
	n := 0
	for _, q := range r.requests {
		if strings.HasPrefix(q, method+" ") && strings.Contains(q, sub) {
			n++
		}
	}
	return n
}

func newTestCCEClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	cred, err := basic.NewCredentialsBuilder().WithAk("ak").WithSk("sk").WithProjectId("project-test").SafeBuild()
	if err != nil {
		t.Fatalf("build credentials: %v", err)
	}
	region, err := cceRegion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve cce region: %v", err)
	}
	hc, err := ccev3.CceClientBuilder().
		WithRegion(region).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig().WithHttpRoundTripper(rt)).
		SafeBuild()
	if err != nil {
		t.Fatalf("build cce client: %v", err)
	}
	eipRegion, err := eipregion.SafeValueOf("cn-north-4")
	if err != nil {
		t.Fatalf("resolve eip region: %v", err)
	}
	ehc, err := eipv2.EipClientBuilder().
		WithRegion(eipRegion).
		WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig().WithHttpRoundTripper(rt)).
		SafeBuild()
	if err != nil {
		t.Fatalf("build eip client: %v", err)
	}
	return &Client{cce: ccev3.NewCceClient(hc), eip: eipv2.NewEipClient(ehc)}
}

// tagJSON renders a tag map as the CCE spec.clusterTags array.
func tagJSON(tags map[string]string) string {
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, `{"key":"`+k+`","value":"`+v+`"}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func showClusterBody(tags map[string]string) string {
	return `{"metadata":{"uid":"cluster-1"},"spec":{"clusterTags":` + tagJSON(tags) + `},"status":{"phase":"Available"}}`
}

func desiredTagMap(t *testing.T, clusterName string, userTags map[string]string) map[string]string {
	t.Helper()
	want := map[string]string{}
	for _, tg := range *toClusterTags(clusterName, userTags) {
		if tg.Key != nil && tg.Value != nil {
			want[*tg.Key] = *tg.Value
		}
	}
	return want
}

func TestReconcileClusterTags(t *testing.T) {
	ctx := context.Background()

	t.Run("in sync issues no mutation", func(t *testing.T) {
		want := desiredTagMap(t, "demo", map[string]string{"env": "prod"})
		rt := &recordingCCERT{t: t, routes: []cceRoute{
			{method: http.MethodGet, sub: "/clusters/cluster-1", body: showClusterBody(want)},
		}}
		if err := newTestCCEClient(t, rt).ReconcileClusterTags(ctx, "cluster-1", "demo", map[string]string{"env": "prod"}); err != nil {
			t.Fatalf("ReconcileClusterTags: %v", err)
		}
		if rt.count(http.MethodPost, "tags/create") != 0 || rt.count(http.MethodPost, "tags/delete") != 0 {
			t.Errorf("expected no tag mutation when in sync, got %v", rt.requests)
		}
	})

	t.Run("drift deletes before creating, never the owned tag", func(t *testing.T) {
		drifted := desiredTagMap(t, "demo", map[string]string{"env": "prod"})
		delete(drifted, "env")      // missing -> must be created
		drifted["stale"] = "remove" // extra -> must be deleted
		rt := &recordingCCERT{t: t, routes: []cceRoute{
			{method: http.MethodGet, sub: "/clusters/cluster-1", body: showClusterBody(drifted)},
			{method: http.MethodPost, sub: "tags/delete", body: `{}`},
			{method: http.MethodPost, sub: "tags/create", body: `{}`},
		}}
		if err := newTestCCEClient(t, rt).ReconcileClusterTags(ctx, "cluster-1", "demo", map[string]string{"env": "prod"}); err != nil {
			t.Fatalf("ReconcileClusterTags: %v", err)
		}
		del := rt.index(http.MethodPost, "tags/delete")
		cre := rt.index(http.MethodPost, "tags/create")
		if del < 0 || cre < 0 {
			t.Fatalf("expected both a delete and a create, got %v", rt.requests)
		}
		if del > cre {
			t.Errorf("deletions must run before creations, got %v", rt.requests)
		}
		if !strings.Contains(rt.bodies[del], "stale") {
			t.Errorf("delete body should carry the stale tag, got %s", rt.bodies[del])
		}
		if !strings.Contains(rt.bodies[cre], "env") {
			t.Errorf("create body should carry the missing tag, got %s", rt.bodies[cre])
		}
		if strings.Contains(rt.bodies[del], "cluster-api-provider-cce.cluster.demo") {
			t.Errorf("the owned tag must never be deleted, got %s", rt.bodies[del])
		}
	})
}

func TestScaleNodePool(t *testing.T) {
	ctx := context.Background()

	t.Run("encodes desired count", func(t *testing.T) {
		rt := &recordingCCERT{t: t, routes: []cceRoute{
			{method: http.MethodPost, sub: "/scale", body: `{}`},
		}}
		if err := newTestCCEClient(t, rt).ScaleNodePool(ctx, "cluster-1", "nodepool-1", 3); err != nil {
			t.Fatalf("ScaleNodePool: %v", err)
		}
		if rt.count(http.MethodPost, "/scale") != 1 {
			t.Fatalf("expected one scale request, got %v", rt.requests)
		}
		if !strings.Contains(rt.bodies[0], `"desiredNodeCount":3`) {
			t.Errorf("scale body must carry desiredNodeCount=3, got %s", rt.bodies[0])
		}
	})

	t.Run("scale-no-op is treated as success", func(t *testing.T) {
		rt := &recordingCCERT{t: t, routes: []cceRoute{
			{method: http.MethodPost, sub: "/scale", status: http.StatusBadRequest,
				body: `{"error_code":"CCE_CM.0004","error_msg":"No scale task needed with desired node count 1"}`},
		}}
		if err := newTestCCEClient(t, rt).ScaleNodePool(ctx, "cluster-1", "nodepool-1", 1); err != nil {
			t.Errorf("a scale-no-op must be swallowed, got %v", err)
		}
	})
}

func TestDeleteClusterOptions(t *testing.T) {
	rt := &recordingCCERT{t: t, routes: []cceRoute{
		{method: http.MethodDelete, sub: "/clusters/cluster-1", body: `{}`},
	}}
	err := newTestCCEClient(t, rt).DeleteCluster(context.Background(), DeleteClusterInput{
		ClusterID: "cluster-1", DeleteEVS: true, DeleteENI: true, DeleteELB: true,
		OnDemandNodePolicy: "delete",
	})
	if err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}
	if len(rt.requests) != 1 || !strings.HasPrefix(rt.requests[0], http.MethodDelete) {
		t.Fatalf("expected one DELETE, got %v", rt.requests)
	}
	target := rt.requests[0]
	for _, want := range []string{"cluster-1", "block"} {
		if !strings.Contains(target, want) {
			t.Errorf("delete request should carry %q, got %s", want, target)
		}
	}
}

func TestGetClusterKubeconfigOverlaysExternalEndpoint(t *testing.T) {
	ctx := context.Background()
	// Minimal cert response: one cluster/user/context; the CA/cert bytes are
	// opaque base64 (the assembler does not validate PEM).
	certBody := `{"clusters":[{"name":"c1","cluster":{"server":"https://10.0.1.17:5443","certificate-authority-data":"QUJD"}}],` +
		`"users":[{"name":"u1","user":{"client-certificate-data":"QUJD","client-key-data":"QUJD"}}],` +
		`"contexts":[{"name":"ctx","context":{"cluster":"c1","user":"u1"}}],"current-context":"ctx"}`
	rt := &recordingCCERT{t: t, routes: []cceRoute{
		{method: http.MethodPost, sub: "clustercert", body: certBody},
		{method: http.MethodGet, sub: "/clusters/cluster-1", body: `{"metadata":{"uid":"cluster-1"},"status":{"phase":"Available","endpoints":[{"url":"https://1.2.3.4:5443","type":"External"}]}}`},
	}}

	// durationDays=0 is out of range and must be clamped to 1.
	kube, err := newTestCCEClient(t, rt).GetClusterKubeconfig(ctx, "cluster-1", 0)
	if err != nil {
		t.Fatalf("GetClusterKubeconfig: %v", err)
	}
	certIdx := rt.index(http.MethodPost, "clustercert")
	if certIdx < 0 {
		t.Fatalf("expected a CreateKubernetesClusterCert call, got %v", rt.requests)
	}
	if !strings.Contains(rt.bodies[certIdx], `"duration":1`) {
		t.Errorf("durationDays=0 must be clamped to 1, got body %s", rt.bodies[certIdx])
	}
	if !strings.Contains(kube, "https://1.2.3.4:5443") {
		t.Errorf("kubeconfig server must be overlaid with the External endpoint, got:\n%s", kube)
	}
	if strings.Contains(kube, "https://10.0.1.17:5443") {
		t.Errorf("the stale cert server must be replaced, got:\n%s", kube)
	}
}

func TestBindAndUnbindClusterEip(t *testing.T) {
	ctx := context.Background()
	t.Run("bind creates, tags and binds an EIP", func(t *testing.T) {
		rt := &recordingCCERT{t: t, routes: []cceRoute{
			{method: http.MethodPost, sub: "/publicips", body: `{"publicip":{"id":"eip-1","public_ip_address":"203.0.113.9"}}`},
			{method: http.MethodPost, sub: "/tags", body: `{}`},
			{method: http.MethodPut, sub: "mastereip", body: `{}`},
		}}
		eipID, addr, err := newTestCCEClient(t, rt).BindClusterEip(ctx, "cluster-1", "demo")
		if err != nil {
			t.Fatalf("BindClusterEip: %v", err)
		}
		if eipID != "eip-1" || addr != "203.0.113.9" {
			t.Errorf("got (%q,%q), want (eip-1,203.0.113.9)", eipID, addr)
		}
		if rt.index(http.MethodPut, "mastereip") < 0 {
			t.Errorf("expected an UpdateClusterEip(bind) call, got %v", rt.requests)
		}
	})

	t.Run("unbind unbinds then releases", func(t *testing.T) {
		rt := &recordingCCERT{t: t, routes: []cceRoute{
			{method: http.MethodPut, sub: "mastereip", body: `{}`},
			{method: http.MethodDelete, sub: "/publicips/eip-1", body: `{}`},
		}}
		if err := newTestCCEClient(t, rt).UnbindClusterEip(ctx, "cluster-1", "eip-1"); err != nil {
			t.Fatalf("UnbindClusterEip: %v", err)
		}
		if rt.index(http.MethodDelete, "/publicips/eip-1") < 0 {
			t.Errorf("expected the EIP to be released, got %v", rt.requests)
		}
	})
}

func TestDeleteNodePoolToleratesAlreadyDeleting(t *testing.T) {
	rt := &recordingCCERT{t: t, routes: []cceRoute{
		{method: http.MethodDelete, sub: "/nodepools/np-1", status: http.StatusForbidden,
			body: `{"error_code":"CCE.01403003","error_msg":"Nodepool phase is Deleting, forbidden to delete nodepool"}`},
	}}
	if err := newTestCCEClient(t, rt).DeleteNodePool(context.Background(), "cluster-1", "np-1"); err != nil {
		t.Errorf("an already-deleting pool must be a no-op, got %v", err)
	}
}
