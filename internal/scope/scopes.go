/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package scope

import (
	"context"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
)

// ClusterScope wraps a CCECluster (InfrastructureCluster).
type ClusterScope struct {
	Client     client.Client
	Cluster    *clusterv1.Cluster
	CCECluster *infrav1beta1.CCECluster
	patch      *PatchHelper
}

// NewClusterScope builds a ClusterScope.
func NewClusterScope(ctx context.Context, c client.Client, cluster *clusterv1.Cluster, cceCluster *infrav1beta1.CCECluster) (*ClusterScope, error) {
	p, err := NewPatchHelper(cceCluster, c)
	if err != nil {
		return nil, err
	}
	return &ClusterScope{Client: c, Cluster: cluster, CCECluster: cceCluster, patch: p}, nil
}

// Region returns the configured region.
func (s *ClusterScope) Region() string { return s.CCECluster.Spec.Region }

// Close persists the CCECluster.
func (s *ClusterScope) Close(ctx context.Context) error {
	return s.patch.Patch(ctx, s.CCECluster)
}

// ControlPlaneScope wraps a CCEManagedControlPlane (ControlPlane).
type ControlPlaneScope struct {
	Client       client.Client
	Cluster      *clusterv1.Cluster
	ControlPlane *controlplanev1beta1.CCEManagedControlPlane
	Credentials  *Credentials
	patch        *PatchHelper
}

// NewControlPlaneScope builds a ControlPlaneScope.
func NewControlPlaneScope(ctx context.Context, c client.Client, cluster *clusterv1.Cluster, cp *controlplanev1beta1.CCEManagedControlPlane, creds *Credentials) (*ControlPlaneScope, error) {
	p, err := NewPatchHelper(cp, c)
	if err != nil {
		return nil, err
	}
	return &ControlPlaneScope{Client: c, Cluster: cluster, ControlPlane: cp, Credentials: creds, patch: p}, nil
}

// Close persists the control plane (status subresource).
func (s *ControlPlaneScope) Close(ctx context.Context) error {
	return s.patch.Patch(ctx, s.ControlPlane)
}

// MachinePoolScope wraps a CCEManagedMachinePool (InfrastructureMachinePool).
type MachinePoolScope struct {
	Client      client.Client
	MachinePool *infrav1beta1.CCEManagedMachinePool
	Credentials *Credentials
	patch       *PatchHelper
}

// NewMachinePoolScope builds a MachinePoolScope.
func NewMachinePoolScope(ctx context.Context, c client.Client, mp *infrav1beta1.CCEManagedMachinePool, creds *Credentials) (*MachinePoolScope, error) {
	p, err := NewPatchHelper(mp, c)
	if err != nil {
		return nil, err
	}
	return &MachinePoolScope{Client: c, MachinePool: mp, Credentials: creds, patch: p}, nil
}

// Close persists the machine pool (status subresource).
func (s *MachinePoolScope) Close(ctx context.Context) error {
	return s.patch.Patch(ctx, s.MachinePool)
}
