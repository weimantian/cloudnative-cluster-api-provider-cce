/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

var (
	testEnv   *envtest.Environment
	k8sClient client.Client
	restCfg   *rest.Config
	// fakeSvc and fakeNet are shared across the controller tests; reset per
	// test as needed.
	fakeSvc *fakes.FakeCCEService
	fakeNet *fakes.FakeNetworkValidator
)

func TestMain(m *testing.M) {
	ctrl.SetLogger(logzap.New(logzap.UseDevMode(true)))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "test", "capi-crds"), // CAPI core CRDs (Cluster/MachinePool)
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	restCfg, err = testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start envtest environment: %v\n", err)
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := clusterv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := infrav1beta1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := controlplanev1beta1.AddToScheme(scheme); err != nil {
		panic(err)
	}

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		panic(err)
	}

	// Register provider feature gates so tests can toggle them (e.g. B3
	// NodePoolAutoscaling).
	if err := features.RegisterFeatureGates(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register feature gates: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := testEnv.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to stop envtest environment: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// --- test helpers ---

// newTestCluster returns a CAPI Cluster with a CCECluster shell (infra) and a
// CCEManagedControlPlane wired via refs. InfrastructureProvisioned defaults
// true so the control plane reconcile can proceed.
func newTestCluster(t *testing.T, ns string) (*clusterv1.Cluster, *infrav1beta1.CCECluster, *controlplanev1beta1.CCEManagedControlPlane) {
	t.Helper()
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: ns},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: infrav1beta1.GroupVersion.Group,
				Kind:     "CCECluster",
				Name:     "test-cluster",
			},
			ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: controlplanev1beta1.GroupVersion.Group,
				Kind:     "CCEManagedControlPlane",
				Name:     "test-cluster-control-plane",
			},
		},
	}
	// Create the owner Cluster first so the ownerRef UID is valid.
	if err := k8sClient.Create(context.Background(), cluster); err != nil {
		t.Fatalf("failed to create Cluster: %v", err)
	}
	ownerRef := metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}
	cceCluster := &infrav1beta1.CCECluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cluster",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: infrav1beta1.CCEClusterSpec{
			Region: "cn-north-4",
			Network: common.NetworkSpec{
				VPC:     common.VPC{ID: "vpc-1"},
				Subnets: []common.Subnet{{ID: "sub-1"}},
			},
		},
	}
	cp := &controlplanev1beta1.CCEManagedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-cluster-control-plane",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: controlplanev1beta1.CCEManagedControlPlaneSpec{
			ClusterName: "test-cluster",
			Category:    "Turbo",
			Flavor:      "cce.s2.medium",
			ContainerNetwork: controlplanev1beta1.ContainerNetworkSpec{
				Mode:       "eni",
				ENISubnets: []string{"sub-1"},
			},
			ServiceNetwork: controlplanev1beta1.ServiceNetworkSpec{CIDR: "10.247.0.0/16"},
			EndpointAccess: controlplanev1beta1.EndpointAccessSpec{Public: true},
		},
	}
	for _, o := range []client.Object{cceCluster, cp} {
		if err := k8sClient.Create(context.Background(), o); err != nil {
			t.Fatalf("failed to create %T: %v", o, err)
		}
	}
	return cluster, cceCluster, cp
}

// markInfrastructureProvisioned sets Cluster.Status.Initialization.
func markInfrastructureProvisioned(t *testing.T, cluster *clusterv1.Cluster) {
	t.Helper()
	cluster.Status.Initialization.InfrastructureProvisioned = boolPtr(true)
	if err := k8sClient.Status().Update(context.Background(), cluster); err != nil {
		t.Fatalf("failed to set infrastructure provisioned: %v", err)
	}
}

// createCredentialsSecret creates the per-cluster credentials Secret the
// controllers resolve (<clusterName>-credentials, keys accessKey/secretKey).
func createCredentialsSecret(t *testing.T, ns, clusterName string) {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-credentials",
			Namespace: ns,
		},
		Data: map[string][]byte{"accessKey": []byte("AK"), "secretKey": []byte("SK")},
	}
	if err := k8sClient.Create(context.Background(), secret); err != nil {
		t.Fatalf("failed to create credentials Secret: %v", err)
	}
}

// createNamespace creates a namespace for the test.
func createNamespace(t *testing.T, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := k8sClient.Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("failed to create namespace %s: %v", name, err)
	}
}

// hasFinalizer reports whether the list contains the given finalizer.
func hasFinalizer(finalizers []string, name string) bool {
	for _, f := range finalizers {
		if f == name {
			return true
		}
	}
	return false
}

func boolPtr(b bool) *bool { return &b }
