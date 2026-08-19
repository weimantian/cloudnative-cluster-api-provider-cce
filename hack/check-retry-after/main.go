// check-retry-after triggers CCE API throttling and captures the response
// headers (Retry-After / x-ratelimit-*) to answer questionnaire Q14.
//
// Usage: CLOUD_SDK_AK=... CLOUD_SDK_SK=... go run ./hack/check-retry-after
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	ccev3 "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
	cceRegion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/region"
)

type captureRT struct {
	mu    sync.Mutex
	seen  map[string]int // header name -> count
	stats map[int]int    // status code -> count
}

func (c *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if resp != nil {
		c.mu.Lock()
		c.stats[resp.StatusCode]++
		for k, v := range resp.Header {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "retry") || strings.Contains(lk, "ratelimit") || strings.Contains(lk, "x-rat") {
				c.seen[k+"="+strings.Join(v, ",")]++
			}
		}
		c.mu.Unlock()
	}
	return resp, err
}

func main() {
	cred, err := basic.NewCredentialsBuilder().
		WithAk(os.Getenv("CLOUD_SDK_AK")).WithSk(os.Getenv("CLOUD_SDK_SK")).SafeBuild()
	must(err)
	region, err := cceRegion.SafeValueOf("cn-north-4")
	must(err)

	rt := &captureRT{seen: map[string]int{}, stats: map[int]int{}}
	hc := config.DefaultHttpConfig()
	hc.RoundTripper = rt

	cc, err := ccev3.CceClientBuilder().
		WithRegion(region).WithCredential(cred).WithHttpConfig(hc).SafeBuild()
	must(err)
	client := ccev3.NewCceClient(cc)

	// Baseline: a few warm-up calls, then a burst of concurrent ListClusters
	// to cross the read throttle (~70 req/s measured earlier).
	const workers, perWorker = 10, 200 // 2000 requests
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_, _ = client.ListClusters(&model.ListClustersRequest{})
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	fmt.Printf("burst: %d calls in %v (%.0f req/s)\n", workers*perWorker, elapsed.Round(time.Millisecond), float64(workers*perWorker)/elapsed.Seconds())
	fmt.Println("status codes:", rt.stats)
	fmt.Println("retry/ratelimit headers observed:")
	for h, n := range rt.seen {
		fmt.Printf("  %q x%d\n", h, n)
	}
	if len(rt.seen) == 0 {
		fmt.Println("  (none) — throttled responses carry no Retry-After / x-ratelimit header")
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
