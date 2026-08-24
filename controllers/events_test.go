/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"strings"
	"testing"

	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/credentials"
	cceService "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/services/cce"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/test/fakes"
)

// TestControlPlaneReconcilerEmitsEvents verifies the control plane reconciler
// emits Kubernetes events (previously it emitted none) at lifecycle
// transitions: cluster created, cluster available, kubeconfig generated.
func TestControlPlaneReconcilerEmitsEvents(t *testing.T) {
	ctx := context.Background()
	ns := "cp-test-events"
	createNamespace(t, ns)

	cluster, _, cp := newTestCluster(t, ns)
	createCredentialsSecret(t, ns, "test-cluster")
	markInfrastructureProvisioned(t, cluster)

	fakeSvc := fakes.NewFakeCCEService()
	recorder := record.NewFakeRecorder(20)
	r := &CCEManagedControlPlaneReconciler{
		Client:   k8sClient,
		Recorder: recorder,
		ServiceFactory: func(_ string, _ *credentials.Credentials) (cceService.Service, error) {
			return fakeSvc, nil
		},
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	events := drainEvents(recorder)
	assertHasEvent(t, events, "Normal ClusterCreated")
	assertHasEvent(t, events, "Normal ClusterAvailable")
	assertHasEvent(t, events, "Normal KubeconfigGenerated")
}

func drainEvents(recorder *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case e := <-recorder.Events:
			events = append(events, e)
		default:
			return events
		}
	}
}

func assertHasEvent(t *testing.T, events []string, want string) {
	t.Helper()
	for _, e := range events {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Errorf("expected event containing %q, got %v", want, events)
}
