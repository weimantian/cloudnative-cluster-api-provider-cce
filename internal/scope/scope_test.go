/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package scope

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveCredentialsFromSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "c-credentials", Namespace: "ns"},
		Data:       map[string][]byte{"accessKey": []byte("AK"), "secretKey": []byte("SK")},
	}).Build()

	creds, err := ResolveCredentials(context.Background(), c, "ns", "c-credentials")
	if err != nil {
		t.Fatalf("ResolveCredentials failed: %v", err)
	}
	if creds.AccessKey != "AK" || creds.SecretKey != "SK" {
		t.Errorf("unexpected creds: %+v", creds)
	}
}

func TestResolveCredentialsMissingSecretIsError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// A referenced-but-missing Secret must be an error, NOT a silent env
	// fallback (cross-tenant safety).
	if _, err := ResolveCredentials(context.Background(), c, "ns", "missing"); err == nil {
		t.Error("expected error for missing referenced Secret")
	}
}

func TestResolveCredentialsSecretMissingKeys(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "c-credentials", Namespace: "ns"},
		Data:       map[string][]byte{"accessKey": []byte("AK")}, // missing secretKey
	}).Build()

	if _, err := ResolveCredentials(context.Background(), c, "ns", "c-credentials"); err == nil {
		t.Error("expected error when secretKey is missing")
	}
}

func TestResolveCredentialsEnvFallbackOnlyWhenUnreferenced(t *testing.T) {
	t.Setenv("CLOUD_SDK_AK", "envAK")
	t.Setenv("CLOUD_SDK_SK", "envSK")
	defer os.Unsetenv("CLOUD_SDK_AK")
	defer os.Unsetenv("CLOUD_SDK_SK")

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Empty secretName => env fallback allowed.
	creds, err := ResolveCredentials(context.Background(), c, "ns", "")
	if err != nil {
		t.Fatalf("env fallback failed: %v", err)
	}
	if creds.AccessKey != "envAK" || creds.SecretKey != "envSK" {
		t.Errorf("unexpected env creds: %+v", creds)
	}
}
