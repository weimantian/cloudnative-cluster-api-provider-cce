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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
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

// ===== P1-8 scope struct tests =====

func newCluster() *clusterv1.Cluster {
	return &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns1"}}
}

func newCCECluster() *infrav1beta2.CCECluster {
	return &infrav1beta2.CCECluster{ObjectMeta: metav1.ObjectMeta{Name: "cce1", Namespace: "ns1"}}
}

func newCCM() *controlplanev1beta2.CCEManagedControlPlane {
	return &controlplanev1beta2.CCEManagedControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "cp1", Namespace: "ns1"}}
}

func newCMP() *infrav1beta2.CCEManagedMachinePool {
	return &infrav1beta2.CCEManagedMachinePool{ObjectMeta: metav1.ObjectMeta{Name: "mp1", Namespace: "ns1"}}
}

func newFakeClient() client.Client {
	scheme := runtime.NewScheme()
	_ = clusterv1.AddToScheme(scheme)
	_ = infrav1beta2.AddToScheme(scheme)
	_ = controlplanev1beta2.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

func TestNewCCEClusterScope_RejectsNil(t *testing.T) {
	cases := map[string]func() error{
		"nil cluster": func() error {
			_, err := NewCCEClusterScope(CCEClusterScopeParams{Client: newFakeClient(), Cluster: nil, CCECluster: newCCECluster(), ControllerName: "x"})
			return err
		},
		"nil CCECluster": func() error {
			_, err := NewCCEClusterScope(CCEClusterScopeParams{Client: newFakeClient(), Cluster: newCluster(), CCECluster: nil, ControllerName: "x"})
			return err
		},
		"empty controllerName": func() error {
			_, err := NewCCEClusterScope(CCEClusterScopeParams{Client: newFakeClient(), Cluster: newCluster(), CCECluster: newCCECluster(), ControllerName: ""})
			return err
		},
		"nil client": func() error {
			_, err := NewCCEClusterScope(CCEClusterScopeParams{Client: nil, Cluster: newCluster(), CCECluster: newCCECluster(), ControllerName: "x"})
			return err
		},
	}
	for name, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestNewCCEClusterScope_HappyPath(t *testing.T) {
	s, err := NewCCEClusterScope(CCEClusterScopeParams{
		Client: newFakeClient(), Cluster: newCluster(), CCECluster: newCCECluster(),
		ControllerName: "ccecluster",
	})
	if err != nil {
		t.Fatalf("NewCCEClusterScope: %v", err)
	}
	if s.Name() != "cce1" || s.Namespace() != "ns1" {
		t.Errorf("Name/Namespace: got %s/%s", s.Name(), s.Namespace())
	}
	if s.ControllerName() != "ccecluster" {
		t.Errorf("ControllerName: %s", s.ControllerName())
	}
	if s.InfraClusterName() != "c1" {
		t.Errorf("InfraClusterName: %s", s.InfraClusterName())
	}
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewCCMScope_HappyPath(t *testing.T) {
	s, err := NewCCEManagedControlPlaneScope(CCEManagedControlPlaneScopeParams{
		Client: newFakeClient(), Cluster: newCluster(), CCEManagedControlPlane: newCCM(),
		ControllerName: "ccm",
	})
	if err != nil {
		t.Fatalf("NewCCMScope: %v", err)
	}
	if s.Name() != "cp1" {
		t.Errorf("Name: %s", s.Name())
	}
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewCMPScope_HappyPath(t *testing.T) {
	s, err := NewCCEManagedMachinePoolScope(CCEManagedMachinePoolScopeParams{
		Client: newFakeClient(), Cluster: newCluster(), CCEManagedMachinePool: newCMP(),
		ControllerName: "cmp",
	})
	if err != nil {
		t.Fatalf("NewCMPScope: %v", err)
	}
	if s.Name() != "mp1" {
		t.Errorf("Name: %s", s.Name())
	}
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewGlobalScope_RejectsNil(t *testing.T) {
	if _, err := NewGlobalScope(GlobalScopeParams{Region: "", ControllerName: "x"}); err == nil {
		t.Error("expected error for empty region")
	}
	if _, err := NewGlobalScope(GlobalScopeParams{Region: "cn-north-4", ControllerName: ""}); err == nil {
		t.Error("expected error for empty controllerName")
	}
}

func TestNewGlobalScope_HappyPath(t *testing.T) {
	s, err := NewGlobalScope(GlobalScopeParams{Region: "cn-north-4", ControllerName: "gc"})
	if err != nil {
		t.Fatalf("NewGlobalScope: %v", err)
	}
	if s.Region() != "cn-north-4" {
		t.Errorf("Region: %s", s.Region())
	}
	if s.ControllerName() != "gc" {
		t.Errorf("ControllerName: %s", s.ControllerName())
	}
}
