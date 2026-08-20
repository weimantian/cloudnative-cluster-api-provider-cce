/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
)

// GarbageCollector periodically sweeps the CCE account for orphaned clusters:
// CCE clusters carrying the owned tag whose Cluster CR no longer exists in the
// management cluster. This is the force-delete / out-of-band-deletion leak:
// once a Cluster CR's finalizer is removed while the CCE cluster still exists,
// no reconcile loop is left to delete it, so the cluster (and its EIP/EVS/ELB/
// ECS nodes) would leak and keep billing.
//
// It mirrors CAPA's ExternalResourceGC (owned-tag orphan detection + aggregated
// delete), scoped to the CCE cluster resource — DeleteCluster's cascade options
// then clean the whole sub-tree (EIP/EVS/ELB/ECS/node pools).
type GarbageCollector struct {
	Client client.Client

	// ServiceFactory builds the CCE service for the sweep. The sweep is
	// account-wide, so it uses the controller-default (env) credentials rather
	// than a per-cluster Secret/identity. Overridden in tests with a fake.
	ServiceFactory func(regionID, ak, sk string) (cceService.Service, error)

	// Region the sweep enumerates (CCE is regional; a sweep covers one region).
	Region string

	// Interval between sweeps.
	Interval time.Duration

	Log logr.Logger
}

// NeedLeaderElection implements the LeaderElectionRunnable marker so the GC
// only runs on the leader, like the controllers.
func (g *GarbageCollector) NeedLeaderElection() bool { return true }

// Start runs the periodic sweep until ctx is cancelled.
func (g *GarbageCollector) Start(ctx context.Context) error {
	g.Log.Info("starting CCE garbage collector", "region", g.Region, "interval", g.Interval)
	g.sweep(ctx)
	ticker := time.NewTicker(g.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			g.sweep(ctx)
		}
	}
}

// sweep enumerates owned-tagged CCE clusters, finds those without a Cluster CR,
// and deletes them. Errors are aggregated per-resource (never abort the whole
// sweep): one failing deletion must not block the others.
func (g *GarbageCollector) sweep(ctx context.Context) {
	log := g.Log

	creds, err := scope.ResolveCredentials(ctx, g.Client, "", "")
	if err != nil {
		log.Info("garbage collector: no controller credentials, skipping sweep", "reason", err.Error())
		return
	}
	svc, err := g.ServiceFactory(g.Region, creds.AccessKey, creds.SecretKey)
	if err != nil {
		log.Error(err, "garbage collector: failed to build CCE service")
		return
	}

	clusters, err := svc.ListClusters(ctx)
	if err != nil {
		log.Error(err, "garbage collector: failed to list CCE clusters")
		return
	}

	// Index the Cluster CRs that should exist.
	list := &clusterv1.ClusterList{}
	if err := g.Client.List(ctx, list); err != nil {
		log.Error(err, "garbage collector: failed to list Cluster CRs")
		return
	}
	wanted := make(map[string]struct{}, len(list.Items))
	for i := range list.Items {
		wanted[list.Items[i].Name] = struct{}{}
	}

	for _, c := range clusters {
		name := ownedClusterName(c.Tags)
		if name == "" {
			continue // not provider-owned; leave it alone
		}
		if _, ok := wanted[name]; ok {
			continue // still tracked by a Cluster CR
		}
		log.Info("garbage collector: deleting orphaned CCE cluster",
			"clusterID", c.ClusterID, "name", name)
		// Cascade options mirror the control plane delete path (questionnaire
		// Q8): delete EIP/ENI/ELB, on-demand nodes deleted, periodic nodes reset.
		if err := svc.DeleteCluster(ctx, cceService.DeleteClusterInput{
			ClusterID:          c.ClusterID,
			DeleteEVS:          true,
			DeleteENI:          true,
			DeleteELB:          true,
			OnDemandNodePolicy: "delete",
			PeriodicNodePolicy: "reset",
		}); err != nil {
			log.Error(err, "garbage collector: failed to delete orphaned CCE cluster",
				"clusterID", c.ClusterID, "name", name)
		}
	}
}

// ownedClusterName returns the cluster name if tags carry the provider's owned
// tag (cluster-api-provider-cce.cluster.<name>=owned), else "".
func ownedClusterName(tags map[string]string) string {
	for k, v := range tags {
		if v == "owned" && strings.HasPrefix(k, cceService.OwnedTagPrefix+".") {
			return strings.TrimPrefix(k, cceService.OwnedTagPrefix+".")
		}
	}
	return ""
}

var _ manager.LeaderElectionRunnable = (*GarbageCollector)(nil)
