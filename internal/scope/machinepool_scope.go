/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package scope

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// CCEManagedMachinePoolScopeParams is the input for NewCCEManagedMachinePoolScope.
type CCEManagedMachinePoolScopeParams struct {
	Client                client.Client
	Cluster               *clusterv1.Cluster
	CCEManagedMachinePool *infrav1beta2.CCEManagedMachinePool
	ControllerName        string
}

// CCEManagedMachinePoolScope is the per-reconcile context for the
// CCEManagedMachinePool controller. Mirrors CAPA's MachinePoolScope with
// WithStatusObservedGeneration patch option (CAPA 9e9bb6b31 family).
type CCEManagedMachinePoolScope struct {
	log                       logr.Logger
	client                    client.Client
	patchHelper               *patch.Helper
	Cluster                   *clusterv1.Cluster
	CCEManagedMachinePool     *infrav1beta2.CCEManagedMachinePool
	controllerName            string
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
	if params.ControllerName == "" {
		return nil, errors.New("controllerName is required")
	}
	if params.Client == nil {
		return nil, errors.New("client is required")
	}

	helper, err := patch.NewHelper(params.CCEManagedMachinePool, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	return &CCEManagedMachinePoolScope{
		log:                       logf.Log.WithName(params.ControllerName),
		client:                    params.Client,
		patchHelper:               helper,
		Cluster:                   params.Cluster,
		CCEManagedMachinePool:     params.CCEManagedMachinePool,
		controllerName:            params.ControllerName,
		observedGenerationAtStart: params.CCEManagedMachinePool.Status.ObservedGeneration,
	}, nil
}

// Client returns the controller-runtime client.
func (s *CCEManagedMachinePoolScope) Client() client.Client { return s.client }

// Logger returns the per-scope logger.
func (s *CCEManagedMachinePoolScope) Logger() logr.Logger { return s.log }

// Name returns the machine pool name.
func (s *CCEManagedMachinePoolScope) Name() string { return s.CCEManagedMachinePool.Name }

// Namespace returns the machine pool namespace.
func (s *CCEManagedMachinePoolScope) Namespace() string { return s.CCEManagedMachinePool.Namespace }

// ControllerName returns the controller name.
func (s *CCEManagedMachinePoolScope) ControllerName() string { return s.controllerName }

// InfraClusterName returns the CAPI Cluster name.
func (s *CCEManagedMachinePoolScope) InfraClusterName() string { return s.Cluster.Name }

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
