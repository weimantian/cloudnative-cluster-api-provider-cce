/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package v1beta1

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/common"
	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

// TestCCEClusterConversionRoundTrip verifies v1beta1 -> v1beta2 (hub) ->
// v1beta1 preserves spec and status. The v1beta1/v1beta2 types are
// structurally identical, so the JSON-based conversion must be lossless.
func TestCCEClusterConversionRoundTrip(t *testing.T) {
	src := &CCECluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			Labels:    map[string]string{"cluster.x-k8s.io/cluster-name": "test-cluster"},
		},
		Spec: CCEClusterSpec{
			Region: "cn-north-4",
			Network: common.NetworkSpec{
				VPC:     common.VPC{ID: "vpc-1"},
				Subnets: []common.Subnet{{ID: "sub-1", AvailabilityZone: "cn-north-4a"}},
			},
		},
		Status: CCEClusterStatus{
			Ready:     true,
			ClusterID: "cluster-1",
			Initialization: ClusterInitializationStatus{
				Provisioned: true,
			},
			Conditions: []metav1.Condition{{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
				Reason: "InfrastructureReady",
			}},
		},
	}

	hub := &infrav1beta2.CCECluster{}
	if err := src.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}
	if hub.Spec.Region != "cn-north-4" || hub.Status.ClusterID != "cluster-1" {
		t.Fatalf("hub conversion lost data: %+v", hub)
	}

	back := &CCECluster{}
	if err := back.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}
	if !reflect.DeepEqual(src.Spec, back.Spec) || !reflect.DeepEqual(src.Status, back.Status) {
		t.Errorf("round-trip mismatch:\n src=%+v\n back=%+v", src, back)
	}
}
