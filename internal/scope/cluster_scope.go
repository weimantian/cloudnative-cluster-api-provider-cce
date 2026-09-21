/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package scope

import (
	"context"

	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// CCEClusterScopeParams is the input for NewCCEClusterScope.
type CCEClusterScopeParams struct {
	Client         client.Client
	Cluster        *clusterv1.Cluster
	CCECluster     *infrav1beta2.CCECluster
	ControllerName string
}

// CCEClusterScope is the per-reconcile context for the CCECluster controller.
// Holds the patchHelper + CR references and exposes PatchObject()/Close() for
// the controller's defer to call.
type CCEClusterScope struct {
	patchHelper *patch.Helper
	Cluster     *clusterv1.Cluster
	CCECluster  *infrav1beta2.CCECluster
}

// NewCCEClusterScope builds a new scope for one reconcile iteration.
func NewCCEClusterScope(params CCEClusterScopeParams) (*CCEClusterScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("cluster is required")
	}
	if params.CCECluster == nil {
		return nil, errors.New("CCECluster is required")
	}
	if params.ControllerName == "" {
		return nil, errors.New("controllerName is required")
	}
	if params.Client == nil {
		return nil, errors.New("client is required")
	}

	helper, err := patch.NewHelper(params.CCECluster, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	return &CCEClusterScope{
		patchHelper: helper,
		Cluster:     params.Cluster,
		CCECluster:  params.CCECluster,
	}, nil
}

// PatchObject persists the CCECluster (spec + status).
// start of reconcile (so the controller can compare against GenerationAtStart

// PatchObject persists the CCECluster (spec + status). Note: CCECluster
// does not yet carry a status.observedGeneration field — controllers that
// need obs/gen requeue must use CCEManagedControlPlaneScope instead.
// must use CCEManagedControlPlaneScope instead.
func (s *CCEClusterScope) PatchObject(ctx context.Context) error {
	return s.patchHelper.Patch(ctx, s.CCECluster)
}

// Close is an alias for PatchObject.
func (s *CCEClusterScope) Close(ctx context.Context) error {
	return s.PatchObject(ctx)
}
