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

// CCEManagedMachinePoolScopeParams is the input for NewCCEManagedMachinePoolScope.
type CCEManagedMachinePoolScopeParams struct {
	Client                client.Client
	Cluster               *clusterv1.Cluster
	CCEManagedMachinePool *infrav1beta2.CCEManagedMachinePool
}

// CCEManagedMachinePoolScope is the per-reconcile context for the
// CCEManagedMachinePool controller, carrying the
// WithStatusObservedGeneration patch option.
type CCEManagedMachinePoolScope struct {
	patchHelper               *patch.Helper
	Cluster                   *clusterv1.Cluster
	CCEManagedMachinePool     *infrav1beta2.CCEManagedMachinePool
	observedGenerationAtStart int64
}

// NewCCEManagedMachinePoolScope builds a new scope for one reconcile iteration.
func NewCCEManagedMachinePoolScope(params CCEManagedMachinePoolScopeParams) (*CCEManagedMachinePoolScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("cluster is required")
	}
	if params.CCEManagedMachinePool == nil {
		return nil, errors.New("CCEManagedMachinePool is required")
	}
	if params.Client == nil {
		return nil, errors.New("client is required")
	}

	helper, err := patch.NewHelper(params.CCEManagedMachinePool, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	return &CCEManagedMachinePoolScope{
		patchHelper:               helper,
		Cluster:                   params.Cluster,
		CCEManagedMachinePool:     params.CCEManagedMachinePool,
		observedGenerationAtStart: params.CCEManagedMachinePool.Status.ObservedGeneration,
	}, nil
}

// GenerationAtStart returns the spec.generation observed at scope build.
func (s *CCEManagedMachinePoolScope) GenerationAtStart() int64 {
	return s.CCEManagedMachinePool.Generation
}

// ObservedGenerationAtStart returns the persisted status.observedGeneration
// at scope build.
func (s *CCEManagedMachinePoolScope) ObservedGenerationAtStart() int64 {
	return s.observedGenerationAtStart
}

// PatchObject persists the CCEManagedMachinePool (spec + status). Atomically
// updates status.observedGeneration via patch.WithStatusObservedGeneration.
func (s *CCEManagedMachinePoolScope) PatchObject(ctx context.Context) error {
	return s.patchHelper.Patch(ctx, s.CCEManagedMachinePool, patch.WithStatusObservedGeneration{})
}

// Close is an alias for PatchObject.
func (s *CCEManagedMachinePoolScope) Close(ctx context.Context) error {
	return s.PatchObject(ctx)
}
