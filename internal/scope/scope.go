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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
