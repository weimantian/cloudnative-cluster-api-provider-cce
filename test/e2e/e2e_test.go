//go:build e2e
// +build e2e

/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package e2e contains the Ginkgo spec that drives a real management cluster
// and a real Huawei Cloud CCE account (mirrors CAPA's suite shape: build tag
// `e2e`, env-gated, Ginkgo Describe/It).
//
// The spec is environment-gated: it skips unless a management cluster
// kubeconfig and the CCE cloud configuration are provided. When run with a
// management cluster (with CAPI + this provider installed) and CCE
// credentials, it exercises the full workload-cluster lifecycle:
// create -> control plane Ready -> node pool -> delete -> gone.
//
// Required environment variables (see docs/smoke-test-checklist.md for the
// CCE prerequisites):
//
//	E2E_MANAGEMENT_KUBECONFIG (or KUBECONFIG) — path to the management cluster kubeconfig
//	CCE_ACCESS_KEY / CCE_SECRET_KEY      — Huawei Cloud AK/SK
//	CCE_E2E_VPC_ID / CCE_E2E_SUBNET_ID   — existing VPC + node subnet in the region
//	CCE_E2E_KEYPAIR                       — ECS keypair name for node SSH access
//
// Optional (defaults): CCE_E2E_REGION (cn-north-4), CCE_E2E_AZ (cn-north-4a),
// CCE_E2E_NODE_FLAVOR (c6.large.2).
package e2e

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	controlplanev1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta2"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

const e2eTimeout = 30 * time.Minute

var _ = Describe("CCE workload cluster lifecycle", func() {
	It("provisions and deletes a workload cluster", func() {
		kubeconfig := os.Getenv("E2E_MANAGEMENT_KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("KUBECONFIG")
		}
		region := envOr("CCE_E2E_REGION", "cn-north-4")
		vpcID := os.Getenv("CCE_E2E_VPC_ID")
		subnetID := os.Getenv("CCE_E2E_SUBNET_ID")
		az := envOr("CCE_E2E_AZ", "cn-north-4a")
		nodeFlavor := envOr("CCE_E2E_NODE_FLAVOR", "c6.large.2")
		keypair := os.Getenv("CCE_E2E_KEYPAIR")

		// Environment gate (mirrors the go-test skip; Ginkgo Skip).
		switch {
		case kubeconfig == "":
			Skip("E2E_MANAGEMENT_KUBECONFIG/KUBECONFIG not set; skipping e2e")
		case vpcID == "" || subnetID == "":
			Skip("CCE_E2E_VPC_ID/CCE_E2E_SUBNET_ID not set; skipping e2e")
		case keypair == "":
			Skip("CCE_E2E_KEYPAIR not set; skipping e2e")
		case os.Getenv("CCE_ACCESS_KEY") == "" || os.Getenv("CCE_SECRET_KEY") == "":
			Skip("CCE_ACCESS_KEY/CCE_SECRET_KEY not set; skipping e2e")
		}

		restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred(), "failed to load management kubeconfig")

		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
		Expect(infrav1beta2.AddToScheme(scheme)).To(Succeed())
		Expect(controlplanev1beta2.AddToScheme(scheme)).To(Succeed())

		c, err := client.New(restCfg, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred(), "failed to build management client")

		ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
		DeferCleanup(cancel)

		const clusterName = "e2e-cluster"
		const namespace = "default"

		// Credentials + empty bootstrap Secret required by the CAPI v1.14
		// MachinePool contract (managed pools carry no bootstrap data).
		Expect(ensureSecret(ctx, c, namespace, clusterName+"-credentials",
			map[string][]byte{"accessKey": []byte(os.Getenv("CCE_ACCESS_KEY")), "secretKey": []byte(os.Getenv("CCE_SECRET_KEY"))})).To(Succeed())
		Expect(ensureSecret(ctx, c, namespace, clusterName+"-bootstrap",
			map[string][]byte{"value": []byte("")})).To(Succeed())

		// Build the workload cluster objects (mirrors config/samples/cluster-template.yaml).
		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: clusterv1.ClusterSpec{
				ClusterNetwork: clusterv1.ClusterNetwork{
					Pods:     clusterv1.NetworkRanges{CIDRBlocks: []string{"10.244.0.0/16"}},
					Services: clusterv1.NetworkRanges{CIDRBlocks: []string{"10.247.0.0/16"}},
				},
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: infrav1beta2.GroupVersion.Group,
					Kind:     "CCECluster",
					Name:     clusterName,
				},
				ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
					APIGroup: controlplanev1beta2.GroupVersion.Group,
					Kind:     "CCEManagedControlPlane",
					Name:     clusterName + "-control-plane",
				},
			},
		}
		Expect(c.Create(ctx, cluster)).To(Succeed(), "failed to create Cluster")
		DeferCleanup(func() { _ = c.Delete(context.Background(), cluster) })

		ownerRef := metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       clusterName,
			UID:        cluster.UID,
		}
		labels := map[string]string{clusterv1.ClusterNameLabel: clusterName}

		objects := []client.Object{
			&infrav1beta2.CCECluster{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{ownerRef}},
				Spec: infrav1beta2.CCEClusterSpec{
					Region: region,
					Network: common.NetworkSpec{
						VPC:     common.VPC{ID: vpcID},
						Subnets: []common.Subnet{{ID: subnetID, AvailabilityZone: az}},
					},
				},
			},
			&controlplanev1beta2.CCEManagedControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-control-plane", Namespace: namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{ownerRef}},
				Spec: controlplanev1beta2.CCEManagedControlPlaneSpec{
					ClusterName: clusterName,
					Category:    "CCE",
					Version:     "v1.33",
					Flavor:      "cce.s2.medium",
					ContainerNetwork: controlplanev1beta2.ContainerNetworkSpec{
						Mode: "vpc-router",
						CIDR: "10.244.0.0/16",
					},
					ServiceNetwork: controlplanev1beta2.ServiceNetworkSpec{CIDR: "10.247.0.0/16"},
					EndpointAccess: controlplanev1beta2.EndpointAccessSpec{Public: false},
					Billing:        controlplanev1beta2.BillingSpec{Mode: 0},
				},
			},
			&clusterv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-pool-0", Namespace: namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{ownerRef}},
				Spec: clusterv1.MachinePoolSpec{
					ClusterName: clusterName,
					Replicas:    int32Ptr(1),
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{
							ClusterName: clusterName,
							Version:     "v1.33.0",
							Bootstrap:   clusterv1.Bootstrap{DataSecretName: ptr(clusterName + "-bootstrap")},
							InfrastructureRef: clusterv1.ContractVersionedObjectReference{
								APIGroup: infrav1beta2.GroupVersion.Group,
								Kind:     "CCEManagedMachinePool",
								Name:     clusterName + "-pool-0",
							},
						},
					},
				},
			},
			&infrav1beta2.CCEManagedMachinePool{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-pool-0", Namespace: namespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{ownerRef}},
				Spec: infrav1beta2.CCEManagedMachinePoolSpec{
					ClusterName:      clusterName,
					NodePoolName:     "pool-0",
					Flavor:           nodeFlavor,
					OS:               "Huawei Cloud EulerOS 2.0",
					RootVolume:       &common.NodeVolume{Size: 40, Type: "GPSSD"},
					DataVolumes:      []common.NodeVolume{{Size: 100, Type: "GPSSD"}},
					SSHKey:           keypair,
					AvailabilityZone: az,
					Replicas:         1,
					BillingMode:      0,
				},
			},
		}
		for _, o := range objects {
			Expect(c.Create(ctx, o)).To(Succeed(), fmt.Sprintf("failed to create %T %s", o, client.ObjectKeyFromObject(o)))
		}

		// Wait for the control plane to become Ready (cluster Available +
		// kubeconfig generated).
		By("waiting for CCEManagedControlPlane to become Ready")
		waitForCondition(ctx, 25*time.Minute, "control plane Ready", func() (bool, error) {
			cp := &controlplanev1beta2.CCEManagedControlPlane{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName + "-control-plane"}, cp); err != nil {
				return false, err
			}
			return cp.Status.Ready, nil
		})

		// Wait for the node pool to reach its desired replicas.
		By("waiting for CCEManagedMachinePool to become Ready")
		waitForCondition(ctx, 25*time.Minute, "node pool Ready", func() (bool, error) {
			pool := &infrav1beta2.CCEManagedMachinePool{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName + "-pool-0"}, pool); err != nil {
				return false, err
			}
			return pool.Status.Ready && pool.Status.Replicas == 1, nil
		})

		// Delete the workload cluster and wait for it to disappear.
		By("deleting the workload cluster")
		Expect(c.Delete(ctx, cluster)).To(Succeed(), "failed to delete Cluster")
		waitForCondition(ctx, 25*time.Minute, "cluster deleted", func() (bool, error) {
			got := &clusterv1.Cluster{}
			err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, got)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
	})
})

func ensureSecret(ctx context.Context, c client.Client, namespace, name string, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       data,
	}
	if err := c.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func waitForCondition(ctx context.Context, timeout time.Duration, desc string, check func() (bool, error)) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			Fail(fmt.Sprintf("timed out waiting for %s", desc))
		}
		done, err := check()
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "waiting for %s: %v\n", desc, err)
		} else if done {
			return
		}
		time.Sleep(10 * time.Second)
	}
	Fail(fmt.Sprintf("timed out waiting for %s", desc))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func int32Ptr(i int32) *int32 { return &i }

func ptr(s string) *string { return &s }
