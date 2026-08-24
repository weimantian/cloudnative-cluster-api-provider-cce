/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package metrics defines the provider's custom Prometheus business metrics
// beyond the controller-runtime defaults (reconcile counts, workqueue depth).
package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// Register registers the provider's custom business metrics on the
// controller-runtime metrics registry. It must be called once after the
// manager (and its client) is constructed and before it starts.
func Register(c client.Client) {
	ctrlmetrics.Registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "cce_managed_clusters",
			Help: "Number of CCECluster objects managed by the provider.",
		}, func() float64 {
			list := &infrav1beta2.CCEClusterList{}
			if err := c.List(context.Background(), list); err != nil {
				return 0
			}
			return float64(len(list.Items))
		}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "cce_managed_machine_pools",
			Help: "Number of CCEManagedMachinePool objects managed by the provider.",
		}, func() float64 {
			list := &infrav1beta2.CCEManagedMachinePoolList{}
			if err := c.List(context.Background(), list); err != nil {
				return 0
			}
			return float64(len(list.Items))
		}),
	)
}
