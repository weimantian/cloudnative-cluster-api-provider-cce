/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/tags"
)

// GarbageCollector periodically sweeps the CCE account for orphaned clusters:
// CCE clusters carrying the owned tag whose Cluster CR no longer exists in the
// management cluster. This is the force-delete / out-of-band-deletion leak:
// once a Cluster CR's finalizer is removed while the CCE cluster still exists,
// no reconcile loop is left to delete it, so the cluster (and its EIP/EVS/ELB/
// ECS nodes) would leak and keep billing.
//
// It mirrors ExternalResourceGC (owned-tag orphan detection + aggregated
// delete), scoped to the CCE cluster resource — DeleteCluster's cascade options
// then clean the whole sub-tree (EIP/EVS/ELB/ECS/node pools).
type GarbageCollector struct {
	Client client.Client

	// ServiceFactory builds the CCE service for the sweep. The sweep is
	// account-wide, so it uses the controller-default (env) credentials rather
	// than a per-cluster Secret/identity. Overridden in tests with a fake.
	ServiceFactory func(regionID string, creds *credentials.Credentials) (cceService.Service, error)

	// Region the sweep enumerates (CCE is regional; a sweep covers one region).
	Region string

	// Interval between sweeps.
	Interval time.Duration

	// ResourceTypes lists the extra resource types the sweeper enumerates
	// beyond clusters: "eip", "evs". Empty = clusters only.
	ResourceTypes []string

	Log logr.Logger
}

// NeedLeaderElection implements the LeaderElectionRunnable marker so the GC
// only runs on the leader, like the controllers.
func (g *GarbageCollector) NeedLeaderElection() bool { return true }

// region returns the region the GC sweeps.
func (g *GarbageCollector) region() string {
	return g.Region
}

// serviceForRegion resolves the GC's region and builds the CCE service for
// the sweep. A GC with no resolvable region is a misconfiguration: fail
// loudly, naming it, instead of letting ServiceFactory("") fail with a
// generic client-build error and silently never deleting anything.
func (g *GarbageCollector) serviceForRegion(creds *credentials.Credentials) (cceService.Service, error) {
	region := g.region()
	if region == "" {
		return nil, errors.New("garbage collector: no GC region configured (set --gc-region)")
	}
	return g.ServiceFactory(region, creds)
}

// Start runs the periodic sweep until ctx is cancelled.
func (g *GarbageCollector) Start(ctx context.Context) error {
	g.Log.Info("starting CCE garbage collector", "region", g.region(), "interval", g.Interval)
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
	svc, err := g.serviceForRegion(&credentials.Credentials{AccessKey: creds.AccessKey, SecretKey: creds.SecretKey})
	if err != nil {
		log.Error(err, "garbage collector: failed to build CCE service")
		return
	}

	clusters, err := svc.ListClusters(ctx)
	if err != nil {
		log.Error(err, "garbage collector: failed to list CCE clusters")
		return
	}

	// Index the Cluster CRs that should exist. We keep the full object
	// (not just the name) so per-cluster annotations can opt out of GC for
	// specific clusters (see skipGCAnnotation).
	list := &clusterv1.ClusterList{}
	if err := g.Client.List(ctx, list); err != nil {
		log.Error(err, "garbage collector: failed to list Cluster CRs")
		return
	}
	// Keyed by Cluster name: the provider owned tag records only the CCE
	// cluster name (the control plane enforces clusterName == Cluster name),
	// not a namespace, so two same-name Clusters in different namespaces are
	// indistinguishable here. That is deliberately fail-safe — any same-name
	// Cluster keeps the cloud cluster alive, at the cost of possibly not
	// collecting a genuine orphan; log the ambiguity so it is diagnosable.
	wanted := make(map[string]*clusterv1.Cluster, len(list.Items))
	for i := range list.Items {
		c := &list.Items[i]
		if prev, ok := wanted[c.Name]; ok && prev.Namespace != c.Namespace {
			log.Info("garbage collector: Cluster name is ambiguous across namespaces, keeping the cloud cluster",
				"name", c.Name, "namespaces", []string{prev.Namespace, c.Namespace})
		}
		wanted[c.Name] = c
	}

	skipCount := 0
	for _, c := range clusters {
		name := tags.OwnedClusterName(c.Tags)
		if name == "" {
			continue // not provider-owned; leave it alone
		}
		wantedCluster, ok := wanted[name]
		if ok {
			if skipGCAnnotation(wantedCluster) {
				skipCount++
				log.Info("garbage collector: skipping orphan cluster (annotation opt-out)",
					"clusterID", c.ClusterID, "name", name,
					"annotation", skipGCAnnotationKey)
				continue
			}
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
	if skipCount > 0 {
		log.Info("garbage collector: skipped orphan sweep via annotation",
			"skippedCount", skipCount, "annotation", skipGCAnnotationKey)
	}

	// Phase 2: orphaned owned-tagged standalone resources (EIP/EVS/VPC/
	// NAT). These are NOT covered by DeleteCluster's cascade options - e.g. a
	// managed NAT EIP whose Cluster CR was force-deleted - and would keep
	// billing. Only resources carrying the provider owned tag whose Cluster
	// CR is gone (and not opted-out) are removed (whitelist-by-tag).
	g.sweepEips(ctx, svc, wanted)
	g.sweepVolumes(ctx, svc, wanted)
	g.sweepVpcs(ctx, svc, wanted)
	g.sweepNatGateways(ctx, svc, wanted)
}

// skipGCAnnotationKey opts a Cluster CR out of GC orphan deletion. Set this
// annotation on a CAPI Cluster to keep its orphaned cloud resources intact
// (e.g. when the cloud resources are managed by an external process or are
// being preserved for forensic analysis). The annotation is honored only
// when the cluster CR still exists but the cloud resource is orphaned.
//
// Mirrors ExternalResourceGCAnnotation opt-out (any truthy value
// means opt-out: true/yes/1/on, case-insensitive).
const skipGCAnnotationKey = "capi-cce/skip-gc"

// skipGCAnnotation reports whether the given Cluster CR requests GC opt-out.
func skipGCAnnotation(c *clusterv1.Cluster) bool {
	if c == nil {
		return false
	}
	v, ok := c.GetAnnotations()[skipGCAnnotationKey]
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "1", "on":
		return true
	}
	return false
}

// Phase-2 sweeps: orphaned owned-tagged standalone resources (EIP/EVS/VPC/
// NAT). These are NOT covered by DeleteCluster's cascade options - e.g. a
// managed NAT EIP whose Cluster CR was force-deleted - and would keep
// billing. Only resources carrying the provider owned tag whose Cluster
// CR is gone are removed (whitelist-by-tag).
//
// Tracked clusters (CR present) skip phase-2 GC: the cloud resource is
// considered managed by the tracked cluster, not orphan. opt-out
// annotation (skipGCAnnotationKey) is consulted in the cluster-phase
// sweep above; for phase-2 resources we keep the simple tracked-skips
// semantics - matches ExternalResourceGCAnnotation which gates only
// the delete-path collection, not the tag-driven orphan scan.

// sweepEips enumerates owned-tagged EIPs whose Cluster CR is gone and
// releases them.
func (g *GarbageCollector) sweepEips(ctx context.Context, svc cceService.Service, wanted map[string]*clusterv1.Cluster) {
	if !slices.Contains(g.ResourceTypes, "eip") {
		return
	}
	eips, err := svc.ListEips(ctx)
	if err != nil {
		g.Log.Error(err, "garbage collector: failed to list EIPs")
		return
	}
	for _, e := range eips {
		name := tags.OwnedClusterName(e.Tags)
		if name == "" {
			continue
		}
		if _, ok := wanted[name]; ok {
			continue
		}
		g.Log.Info("garbage collector: deleting orphaned EIP", "eipID", e.ID, "address", e.Address, "name", name)
		if err := svc.DeleteEip(ctx, e.ID); err != nil {
			g.Log.Error(err, "garbage collector: failed to delete orphaned EIP", "eipID", e.ID)
		}
	}
}

// sweepVolumes enumerates owned-tagged EVS volumes whose Cluster CR is gone
// and deletes them.
func (g *GarbageCollector) sweepVolumes(ctx context.Context, svc cceService.Service, wanted map[string]*clusterv1.Cluster) {
	if !slices.Contains(g.ResourceTypes, "evs") {
		return
	}
	vols, err := svc.ListVolumes(ctx)
	if err != nil {
		g.Log.Error(err, "garbage collector: failed to list volumes")
		return
	}
	for _, v := range vols {
		name := tags.OwnedClusterName(v.Tags)
		if name == "" {
			continue
		}
		if _, ok := wanted[name]; ok {
			continue
		}
		g.Log.Info("garbage collector: deleting orphaned EVS volume", "volumeID", v.ID, "name", name)
		if err := svc.DeleteVolume(ctx, v.ID); err != nil {
			g.Log.Error(err, "garbage collector: failed to delete orphaned EVS volume", "volumeID", v.ID)
		}
	}
}

// sweepVpcs enumerates owned-tagged VPCs whose Cluster CR is gone and
// deletes them. VPCs are free, so this is about resource hygiene rather
// than billing.
func (g *GarbageCollector) sweepVpcs(ctx context.Context, svc cceService.Service, wanted map[string]*clusterv1.Cluster) {
	if !slices.Contains(g.ResourceTypes, "vpc") {
		return
	}
	vpcs, err := svc.ListVpcs(ctx)
	if err != nil {
		g.Log.Error(err, "garbage collector: failed to list VPCs")
		return
	}
	for _, v := range vpcs {
		name := tags.OwnedClusterName(v.Tags)
		if name == "" {
			continue
		}
		if _, ok := wanted[name]; ok {
			continue
		}
		g.Log.Info("garbage collector: deleting orphaned VPC", "vpcID", v.ID, "name", name)
		if err := svc.DeleteVpc(ctx, v.ID); err != nil {
			g.Log.Error(err, "garbage collector: failed to delete orphaned VPC", "vpcID", v.ID)
		}
	}
}

// sweepNatGateways enumerates owned-tagged NAT gateways whose Cluster CR is
// gone and deletes them (SNAT rules first, then the gateway).
func (g *GarbageCollector) sweepNatGateways(ctx context.Context, svc cceService.Service, wanted map[string]*clusterv1.Cluster) {
	if !slices.Contains(g.ResourceTypes, "nat") {
		return
	}
	gateways, err := svc.ListNatGateways(ctx)
	if err != nil {
		g.Log.Error(err, "garbage collector: failed to list NAT gateways")
		return
	}
	for _, gw := range gateways {
		name := tags.OwnedClusterName(gw.Tags)
		if name == "" {
			continue
		}
		if _, ok := wanted[name]; ok {
			continue
		}
		g.Log.Info("garbage collector: deleting orphaned NAT gateway", "gatewayID", gw.ID, "name", name)
		if err := svc.DeleteNatGateway(ctx, gw.ID); err != nil {
			g.Log.Error(err, "garbage collector: failed to delete orphaned NAT gateway", "gatewayID", gw.ID)
		}
	}
}

var _ manager.LeaderElectionRunnable = (*GarbageCollector)(nil)
