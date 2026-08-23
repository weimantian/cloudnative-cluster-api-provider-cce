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

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
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

func TestResolveIdentityStatic(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrav1beta2.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&infrav1beta2.CCEClusterStaticIdentity{
			ObjectMeta: metav1.ObjectMeta{Name: "my-id"},
			Spec:       infrav1beta2.CCEClusterStaticIdentitySpec{SecretRef: "my-secret"},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "cce-provider-system"},
			Data:       map[string][]byte{"accessKey": []byte("AK"), "secretKey": []byte("SK")},
		},
	).Build()

	creds, agency, err := ResolveIdentity(context.Background(), c, "default",
		&corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "my-id"})
	if err != nil {
		t.Fatalf("ResolveIdentity failed: %v", err)
	}
	if creds.AccessKey != "AK" || creds.SecretKey != "SK" || agency != "" {
		t.Errorf("unexpected creds/agency: %+v %q", creds, agency)
	}
}

func TestResolveIdentityAllowedNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrav1beta2.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&infrav1beta2.CCEClusterStaticIdentity{
			ObjectMeta: metav1.ObjectMeta{Name: "my-id"},
			Spec: infrav1beta2.CCEClusterStaticIdentitySpec{
				SecretRef: "my-secret",
				AllowedNamespaces: &infrav1beta2.AllowedNamespaces{
					NamespaceList: []string{"allowed-ns"},
				},
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "cce-provider-system"},
			Data:       map[string][]byte{"accessKey": []byte("AK"), "secretKey": []byte("SK")},
		},
	).Build()

	// A namespace outside the allowlist must be rejected.
	if _, _, err := ResolveIdentity(context.Background(), c, "other-ns",
		&corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "my-id"}); err == nil {
		t.Error("expected allowedNamespaces rejection for other-ns")
	}
	// Allowed namespace passes.
	if _, _, err := ResolveIdentity(context.Background(), c, "allowed-ns",
		&corev1.ObjectReference{Kind: "CCEClusterStaticIdentity", Name: "my-id"}); err != nil {
		t.Errorf("expected allowed namespace to pass, got %v", err)
	}
}

func TestResolveIdentityNilRef(t *testing.T) {
	t.Setenv("CLOUD_SDK_AK", "envAK")
	t.Setenv("CLOUD_SDK_SK", "envSK")
	defer os.Unsetenv("CLOUD_SDK_AK")
	defer os.Unsetenv("CLOUD_SDK_SK")
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	creds, agency, err := ResolveIdentity(context.Background(), c, "ns", nil)
	if err != nil {
		t.Fatalf("ResolveIdentity(nil) failed: %v", err)
	}
	if creds.AccessKey != "envAK" || agency != "" {
		t.Errorf("unexpected: %+v agency=%q", creds, agency)
	}
}
func TestResolveIdentityControllerEnforcesAllowedNamespaces(t *testing.T) {
	t.Setenv("CLOUD_SDK_AK", "envAK")
	t.Setenv("CLOUD_SDK_SK", "envSK")
	defer os.Unsetenv("CLOUD_SDK_AK")
	defer os.Unsetenv("CLOUD_SDK_SK")

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrav1beta2.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&infrav1beta2.CCEClusterControllerIdentity{
			ObjectMeta: metav1.ObjectMeta{Name: "default"},
			Spec: infrav1beta2.CCEClusterControllerIdentitySpec{
				AllowedNamespaces: &infrav1beta2.AllowedNamespaces{NamespaceList: []string{"allowed-ns"}},
			},
		},
	).Build()

	// Disallowed namespace is rejected.
	if _, _, err := ResolveIdentity(context.Background(), c, "other-ns",
		&corev1.ObjectReference{Kind: "CCEClusterControllerIdentity", Name: "default"}); err == nil {
		t.Error("expected allowedNamespaces rejection for other-ns")
	}
	// Allowed namespace passes with env creds.
	creds, _, err := ResolveIdentity(context.Background(), c, "allowed-ns",
		&corev1.ObjectReference{Kind: "CCEClusterControllerIdentity", Name: "default"})
	if err != nil {
		t.Fatalf("expected allowed namespace to pass, got %v", err)
	}
	if creds.AccessKey != "envAK" {
		t.Errorf("expected env creds, got %+v", creds)
	}
}
