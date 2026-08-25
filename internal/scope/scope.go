/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package scope holds the per-object scopes that wrap the CRs, the Kubernetes
// client and the resolved cloud credentials. Pattern follows CAPA
// pkg/cloud/scope and CAPHW pkg/scope (patch helper + Close() = PatchObject).
package scope

import (
	"context"
	"os"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// Credentials are the Huawei Cloud AK/SK used by the services layer.
type Credentials struct {
	AccessKey string
	SecretKey string
}

// ResolveCredentials reads the per-cluster credentials Secret
// (<cluster>-credentials, keys accessKey/secretKey). Environment fallback
// (CLOUD_SDK_AK / CLOUD_SDK_SK) is used ONLY when no Secret name is given;
// when a Secret is explicitly referenced but missing, this is an error rather
// than a silent fallback — otherwise a typo would silently run the provider
// against the global account (cross-tenant risk).
func ResolveCredentials(ctx context.Context, c client.Client, namespace, secretName string) (*Credentials, error) {
	if secretName == "" {
		return credentialsFromEnv()
	}
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: secretName}
	if err := c.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.Errorf("credentials Secret %s not found (create it with keys accessKey/secretKey)", key)
		}
		return nil, errors.Wrapf(err, "failed to read credentials Secret %s", key)
	}
	ak := string(secret.Data["accessKey"])
	sk := string(secret.Data["secretKey"])
	if ak == "" || sk == "" {
		return nil, errors.Errorf("credentials Secret %s must contain accessKey and secretKey", key)
	}
	return &Credentials{AccessKey: ak, SecretKey: sk}, nil
}

func credentialsFromEnv() (*Credentials, error) {
	ak := os.Getenv("CLOUD_SDK_AK")
	sk := os.Getenv("CLOUD_SDK_SK")
	if ak == "" || sk == "" {
		return nil, errors.New("no credentials found: set CLOUD_SDK_AK/CLOUD_SDK_SK or create a per-cluster credentials Secret")
	}
	return &Credentials{AccessKey: ak, SecretKey: sk}, nil
}

// ResolveIdentity resolves a control plane's identityRef into credentials and
// an optional agency name, enforcing allowedNamespaces. When identityRef is
// nil, the controller default identity (env) is used.
func ResolveIdentity(ctx context.Context, c client.Client, namespace string, identityRef *corev1.ObjectReference) (*Credentials, string, error) {
	if identityRef == nil {
		creds, err := credentialsFromEnv()
		return creds, "", err
	}
	switch identityRef.Kind {
	case "CCEClusterControllerIdentity":
		id := &infrav1beta2.CCEClusterControllerIdentity{}
		if err := c.Get(ctx, types.NamespacedName{Name: identityRef.Name}, id); err != nil {
			return nil, "", errors.Wrapf(err, "failed to get CCEClusterControllerIdentity %s", identityRef.Name)
		}
		// Enforce allowedNamespaces exactly like the other identities (the
		// controller identity previously skipped this check, so its
		// allowedNamespaces was silently ignored).
		if err := checkAllowedNamespace(ctx, c, id.Spec.AllowedNamespaces, namespace, identityRef.Name); err != nil {
			return nil, "", err
		}
		creds, err := credentialsFromEnv()
		return creds, "", err
	case "CCEClusterStaticIdentity":
		id := &infrav1beta2.CCEClusterStaticIdentity{}
		if err := c.Get(ctx, types.NamespacedName{Name: identityRef.Name}, id); err != nil {
			return nil, "", errors.Wrapf(err, "failed to get CCEClusterStaticIdentity %s", identityRef.Name)
		}
		if err := checkAllowedNamespace(ctx, c, id.Spec.AllowedNamespaces, namespace, identityRef.Name); err != nil {
			return nil, "", err
		}
		secret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: "cloudnative-cluster-api-provider-cce-system", Name: id.Spec.SecretRef}, secret); err != nil {
			return nil, "", errors.Wrapf(err, "failed to read static identity Secret %s", id.Spec.SecretRef)
		}
		ak, sk := string(secret.Data["accessKey"]), string(secret.Data["secretKey"])
		if ak == "" || sk == "" {
			return nil, "", errors.Errorf("static identity Secret %s must contain accessKey and secretKey", id.Spec.SecretRef)
		}
		return &Credentials{AccessKey: ak, SecretKey: sk}, "", nil
	case "CCEClusterRoleIdentity":
		id := &infrav1beta2.CCEClusterRoleIdentity{}
		if err := c.Get(ctx, types.NamespacedName{Name: identityRef.Name}, id); err != nil {
			return nil, "", errors.Wrapf(err, "failed to get CCEClusterRoleIdentity %s", identityRef.Name)
		}
		if err := checkAllowedNamespace(ctx, c, id.Spec.AllowedNamespaces, namespace, identityRef.Name); err != nil {
			return nil, "", err
		}
		creds, err := credentialsFromEnv()
		return creds, id.Spec.AgencyName, err
	default:
		return nil, "", errors.Errorf("unsupported identityRef kind %q", identityRef.Kind)
	}
}

// checkAllowedNamespace enforces the identity's allowedNamespaces. A nil
// pointer means "any namespace" (CAPA contract); an empty list + empty
// selector means "no namespace".
func checkAllowedNamespace(ctx context.Context, c client.Client, allowed *infrav1beta2.AllowedNamespaces, namespace, identityName string) error {
	if allowed == nil {
		return nil // any namespace
	}
	for _, ns := range allowed.NamespaceList {
		if ns == namespace {
			return nil
		}
	}
	// Label selector over namespaces.
	if len(allowed.Selector.MatchLabels) > 0 || len(allowed.Selector.MatchExpressions) > 0 {
		nsObj := &corev1.Namespace{}
		if err := c.Get(ctx, types.NamespacedName{Name: namespace}, nsObj); err == nil {
			sel, err := metav1.LabelSelectorAsSelector(&allowed.Selector)
			if err != nil {
				return errors.Wrap(err, "invalid identity namespace selector")
			}
			if sel.Matches(labels.Set(nsObj.Labels)) {
				return nil
			}
		}
	}
	return errors.Errorf("namespace %q is not allowed to use identity %q", namespace, identityName)
}
