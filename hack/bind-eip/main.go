/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Command bind-eip binds a public EIP to an existing CCE cluster's API server
// and reports whether the cluster's nodes finished container runtime install.
//
// This is a one-off operations tool (not part of the provider binary). It:
//  1. Shows the cluster's current API server endpoints (ShowClusterEndpoints).
//  2. If no public endpoint exists, creates a 5_bgp EIP and binds it via
//     UpdateClusterEip(action=bind).
//  3. Polls until the public endpoint is exposed.
//  4. Probes https://<publicEndpoint> reachability from this machine.
//  5. Lists cluster nodes and reports each node's phase + runtime so the
//     operator can confirm "node container install is OK".
//
// Usage:
//
//	CCE_DEPLOY_AK=... CCE_DEPLOY_SK=... \
//	  go run ./hack/bind-eip -cluster <cluster-id>
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/hack/internal/hwutil"
)

func main() {
	clusterID := flag.String("cluster", "", "existing CCE cluster ID")
	flag.Parse()
	if *clusterID == "" {
		fatal("usage: go run ./hack/bind-eip -cluster <cluster-id>")
	}

	ak := hwutil.EnvOr("CCE_DEPLOY_AK", "CLOUD_SDK_AK")
	sk := hwutil.EnvOr("CCE_DEPLOY_SK", "CLOUD_SDK_SK")
	region := hwutil.EnvOr("CCE_DEPLOY_REGION", "cn-north-4")
	if ak == "" || sk == "" {
		fatal("CCE_DEPLOY_AK (or CLOUD_SDK_AK) and CCE_DEPLOY_SK (or CLOUD_SDK_SK) must be set")
	}

	ctx := context.Background()

	cc := newCCEClient(region, ak, sk)

	// 1. Current endpoints.
	fmt.Printf("cluster: %s (region %s)\n", *clusterID, region)
	ep, err := cc.ShowClusterEndpoints(&model.ShowClusterEndpointsRequest{ClusterId: *clusterID})
	if err != nil {
		fatalf("ShowClusterEndpoints: %v", err)
	}
	private, public := "", ""
	if ep.Status != nil {
		if ep.Status.PrivateEndpoint != nil {
			private = *ep.Status.PrivateEndpoint
		}
		if ep.Status.PublicEndpoint != nil {
			public = *ep.Status.PublicEndpoint
		}
	}
	fmt.Printf("private endpoint: %q\npublic  endpoint: %q\n", private, public)

	// 2. Bind EIP if no public endpoint yet.
	if public == "" {
		fmt.Println("no public endpoint — creating + binding EIP…")
		eipc, err := hwutil.NewEipClient(region, ak, sk)
		if err != nil {
			fatalf("create EIP: %v", err)
		}
		eipID, eipAddr, err := hwutil.CreatePublicIP(ctx, eipc, "capi-ops-eip")
		if err != nil {
			fatalf("create EIP: %v", err)
		}
		fmt.Printf("created EIP id=%s addr=%s\n", eipID, eipAddr)

		action := model.GetMasterEipRequestSpecActionEnum().BIND
		if _, err := cc.UpdateClusterEip(&model.UpdateClusterEipRequest{
			ClusterId: *clusterID,
			Body: &model.MasterEipRequest{
				Spec: &model.MasterEipRequestSpec{
					Action: &action,
					Spec:   &model.MasterEipRequestSpecSpec{Id: &eipID},
				},
			},
		}); err != nil {
			fatalf("UpdateClusterEip(bind): %v", err)
		}
		fmt.Println("bind request accepted — waiting for public endpoint…")

		for i := 0; i < 30; i++ {
			time.Sleep(20 * time.Second)
			ep, err := cc.ShowClusterEndpoints(&model.ShowClusterEndpointsRequest{ClusterId: *clusterID})
			if err != nil {
				fmt.Printf("  poll %d: ShowClusterEndpoints error: %v\n", i+1, err)
				continue
			}
			if ep.Status != nil && ep.Status.PublicEndpoint != nil && *ep.Status.PublicEndpoint != "" {
				public = *ep.Status.PublicEndpoint
				break
			}
		}
		if public == "" && eipAddr != "" {
			public = fmt.Sprintf("https://%s:5443", eipAddr)
		}
	}
	if public == "" {
		fatal("no public endpoint after bind — check console")
	}
	fmt.Printf("public endpoint: %s\n", public)

	// 3. Probe reachability.
	if !strings.HasPrefix(public, "http") {
		public = "https://" + public
	}
	ok, perr := probeHTTPS(public, 15*time.Second)
	if perr != nil {
		fmt.Printf("probe %s: error (may still be provisioning): %v\n", public, perr)
	} else {
		fmt.Printf("probe %s: reachable=%v\n", public, ok)
	}

	// 4. Node container install status.
	nodes, err := cc.ListNodes(&model.ListNodesRequest{ClusterId: *clusterID})
	if err != nil {
		fatalf("ListNodes: %v", err)
	}
	fmt.Println("---- nodes ----")
	if nodes.Items == nil || len(*nodes.Items) == 0 {
		fmt.Println("(no nodes)")
	}
	allActive := true
	for _, n := range *nodes.Items {
		name, phase, runtime, osName := "-", "-", "-", "-"
		if n.Metadata != nil && n.Metadata.Name != nil {
			name = *n.Metadata.Name
		}
		if n.Status != nil && n.Status.Phase != nil {
			phase = n.Status.Phase.Value()
		}
		if n.Spec != nil && n.Spec.Runtime != nil {
			runtime = runtimeOf(n.Spec.Runtime)
		}
		if n.Spec != nil && n.Spec.Os != nil {
			osName = *n.Spec.Os
		}
		fmt.Printf("node=%s phase=%s runtime=%s os=%s\n", name, phase, runtime, osName)
		if phase != "Active" {
			allActive = false
		}
	}
	if allActive && nodes.Items != nil && len(*nodes.Items) > 0 {
		fmt.Println("RESULT: all nodes Active -> node container install OK")
	} else {
		fmt.Println("RESULT: node container install NOT fully OK (see phase above)")
	}
}

func newCCEClient(region, ak, sk string) *ccev3.CceClient {
	r, err := cceRegion.SafeValueOf(region)
	must(err, "resolve CCE region")
	cred, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	must(err, "build credentials")
	hc, err := ccev3.CceClientBuilder().WithRegion(r).WithCredential(cred).
		WithHttpConfig(config.DefaultHttpConfig()).SafeBuild()
	must(err, "build CCE client")
	return ccev3.NewCceClient(hc)
}

func probeHTTPS(url string, timeout time.Duration) (bool, error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // ops probe
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return true, nil
}

func runtimeOf(r *model.Runtime) string {
	if r == nil {
		return "-"
	}
	name, class := "-", "-"
	if r.Name != nil {
		name = r.Name.Value()
	}
	if r.RuntimeClass != nil {
		class = r.RuntimeClass.Value()
	}
	return name + "/" + class
}

func must(err error, ctx string) {
	if err != nil {
		fatalf("%s: %v", ctx, err)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "ERROR:", msg)
	os.Exit(1)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
