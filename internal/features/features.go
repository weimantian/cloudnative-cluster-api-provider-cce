/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package features registers and exposes the provider feature gates.
package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Every feature gate should add a constant here and register it below.

	// NodePoolAutoscaling enables mapping of CCE node pool autoscaling
	// (autoscaling.enable/min/max) onto the provider CRD. Off by default:
	// scaling is driven solely by CAPI MachinePool replicas (see
	// docs/requirements-design.md FR-2.6, verification item Q3).
	NodePoolAutoscaling featuregate.Feature = "NodePoolAutoscaling"
)

var (
	// featureGates is the set of feature gates for the provider.
	featureGates = map[featuregate.Feature]featuregate.FeatureSpec{
		NodePoolAutoscaling: {Default: false, PreRelease: featuregate.Alpha},
	}

	// featureGatesMutable is the mutable registry used by the manager.
	featureGatesMutable = featuregate.NewFeatureGate()
)

// RegisterFeatureGates registers the provider feature gates globally.
func RegisterFeatureGates() error {
	return featureGatesMutable.Add(featureGates)
}

var _ = runtime.Must // reserved for future gate wiring
