/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/scope"
)

// resolveControlPlaneCredentials resolves the credentials (and the optional
// agency from a CCEClusterRoleIdentity) for a control plane: spec.identityRef
// first, then the per-cluster Secret (<cluster>-credentials), then env.
//
// ALL reconciler paths (create, update and delete) must go through this one
// chain. The delete paths previously resolved only the per-cluster Secret,
// so clusters provisioned via identityRef could never be deleted (the
// missing Secret failed the reconcile forever and the finalizer was never
// removed). The machine pool controller shares the control plane's identity
// as well - mirroring CAPA, where machine pools resolve credentials from the
// cluster scope.
func resolveControlPlaneCredentials(ctx context.Context, c client.Client, cp *controlplanev1beta1.CCEManagedControlPlane) (*scope.Credentials, string, error) {
	if cp.Spec.IdentityRef != nil {
		return scope.ResolveIdentity(ctx, c, cp.Namespace, cp.Spec.IdentityRef)
	}
	creds, err := scope.ResolveCredentials(ctx, c, cp.Namespace, cp.Spec.ClusterName+"-credentials")
	return creds, "", err
}
