/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package metrics

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestRegister(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked on first call: %v", r)
		}
	}()
	Register(fake.NewClientBuilder().Build())

	families, err := ctrlmetrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}
	found := map[string]bool{}
	for _, f := range families {
		found[f.GetName()] = true
	}
	for _, name := range []string{"cce_managed_clusters", "cce_managed_machine_pools"} {
		if !found[name] {
			t.Errorf("metric %q not registered", name)
		}
	}

	// A second Register with the same metric names must panic under
	// MustRegister (duplicate collector registration).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected duplicate Register to panic")
			}
		}()
		Register(fake.NewClientBuilder().Build())
	}()
}
