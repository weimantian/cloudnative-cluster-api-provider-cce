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

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
)

// CCEManagedControlPlaneScopeParams is the input for NewCCEManagedControlPlaneScope.
type CCEManagedControlPlaneScopeParams struct {
	Client                 client.Client
	Cluster                *clusterv1.Cluster
	CCEManagedControlPlane *controlplanev1beta2.CCEManagedControlPlane
	ControllerName         string
}

// CCEManagedControlPlaneScope is the per-reconcile context for the
// CCEManagedControlPlane controller. Mirrors CAPA's ManagedControlPlaneScope
// and carries the WithStatusObservedGeneration patch option to write
// status.observedGeneration atomically (CAPA b5d6d3081).
type CCEManagedControlPlaneScope struct {
	log                    logr.Logger
	client                 client.Client
	patchHelper            *patch.Helper
	Cluster                *clusterv1.Cluster
	CCEManagedControlPlane *controlplanev1beta2.CCEManagedControlPlane
	controllerName         string
	// observedGenerationAtStart is captured at scope build time so the
	// controller can detect spec changes that arrived after the Get and
	// were coalesced into the in-flight work-queue entry.
	observedGenerationAtStart int64
}

// NewCCEManagedControlPlaneScope builds a new scope for one reconcile iteration.
func NewCCEManagedControlPlaneScope(params CCEManagedControlPlaneScopeParams) (*CCEManagedControlPlaneScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("cluster is required")
	}
	if params.CCEManagedControlPlane == nil {
		return nil, errors.New("CCEManagedControlPlane is required")
	}
	if params.ControllerName == "" {
		return nil, errors.New("controllerName is required")
	}
	if params.Client == nil {
		return nil, errors.New("client is required")
	}

	helper, err := patch.NewHelper(params.CCEManagedControlPlane, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	return &CCEManagedControlPlaneScope{
		log:                       logf.Log.WithName(params.ControllerName),
		client:                    params.Client,
		patchHelper:               helper,
		Cluster:                   params.Cluster,
		CCEManagedControlPlane:    params.CCEManagedControlPlane,
		controllerName:            params.ControllerName,
		observedGenerationAtStart: params.CCEManagedControlPlane.Status.ObservedGeneration,
	}, nil
}

// Client returns the controller-runtime client.
func (s *CCEManagedControlPlaneScope) Client() client.Client { return s.client }

// Logger returns the per-scope logger.
func (s *CCEManagedControlPlaneScope) Logger() logr.Logger { return s.log }

// Name returns the control plane name.
func (s *CCEManagedControlPlaneScope) Name() string { return s.CCEManagedControlPlane.Name }

// Namespace returns the control plane namespace.
func (s *CCEManagedControlPlaneScope) Namespace() string { return s.CCEManagedControlPlane.Namespace }

// ControllerName returns the controller name.
func (s *CCEManagedControlPlaneScope) ControllerName() string { return s.controllerName }

// InfraClusterName returns the CAPI Cluster name.
func (s *CCEManagedControlPlaneScope) InfraClusterName() string { return s.Cluster.Name }

// GenerationAtStart returns the spec.generation observed at scope build.
func (s *CCEManagedControlPlaneScope) GenerationAtStart() int64 {
	return s.CCEManagedControlPlane.Generation
}

// ObservedGenerationAtStart returns the persisted status.observedGeneration
// at scope build, used to detect spec changes coalesced into the in-flight
// work-queue entry.
func (s *CCEManagedControlPlaneScope) ObservedGenerationAtStart() int64 {
	return s.observedGenerationAtStart
}

// PatchObject persists the CCEManagedControlPlane (spec + status). The
// status.observedGeneration field is atomically updated to match
// metadata.generation via patch.WithStatusObservedGeneration (CAPA 9e9bb6b31).
func (s *CCEManagedControlPlaneScope) PatchObject(ctx context.Context) error {
	return s.patchHelper.Patch(ctx, s.CCEManagedControlPlane, patch.WithStatusObservedGeneration{})
}

// Close is an alias for PatchObject; mirrors CAPA's scope.Close = PatchObject.
func (s *CCEManagedControlPlaneScope) Close(ctx context.Context) error {
	return s.PatchObject(ctx)
}
