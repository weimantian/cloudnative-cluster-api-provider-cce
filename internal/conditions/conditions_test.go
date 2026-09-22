/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package conditions

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1beta2 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta2"
)

func TestMarkTrueSetsCondition(t *testing.T) {
	c := &infrav1beta2.CCECluster{}
	MarkTrue(c, NetworkReadyCondition, ReconciliationInProgressReason, "validating")

	conds := c.GetConditions()
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	got := conds[0]
	if got.Type != NetworkReadyCondition {
		t.Errorf("condition type = %q, want %q", got.Type, NetworkReadyCondition)
	}
	if got.Status != metav1.ConditionTrue {
		t.Errorf("condition status = %q, want True", got.Status)
	}
	if got.Reason != ReconciliationInProgressReason {
		t.Errorf("condition reason = %q, want %q", got.Reason, ReconciliationInProgressReason)
	}
	if got.Message != "validating" {
		t.Errorf("condition message = %q, want %q", got.Message, "validating")
	}
}

func TestMarkFalseSetsCondition(t *testing.T) {
	c := &infrav1beta2.CCECluster{}
	MarkFalse(c, NetworkReadyCondition, NetworkValidationFailedReason, "bad cidr")

	conds := c.GetConditions()
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	got := conds[0]
	if got.Status != metav1.ConditionFalse {
		t.Errorf("condition status = %q, want False", got.Status)
	}
	if got.Reason != NetworkValidationFailedReason {
		t.Errorf("condition reason = %q, want %q", got.Reason, NetworkValidationFailedReason)
	}
	if got.Message != "bad cidr" {
		t.Errorf("condition message = %q, want %q", got.Message, "bad cidr")
	}
}

func TestMarkTrueIdempotentUpdate(t *testing.T) {
	c := &infrav1beta2.CCECluster{}
	MarkTrue(c, NetworkReadyCondition, "reason-a", "first")
	MarkTrue(c, NetworkReadyCondition, "reason-b", "second")

	conds := c.GetConditions()
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition after update, got %d", len(conds))
	}
	got := conds[0]
	if got.Reason != "reason-b" || got.Message != "second" {
		t.Errorf("condition after update = %q/%q, want reason-b/second", got.Reason, got.Message)
	}
}

func TestMarkTrueMultipleConditions(t *testing.T) {
	c := &infrav1beta2.CCECluster{}
	MarkTrue(c, NetworkReadyCondition, ReconciliationInProgressReason, "a")
	MarkFalse(c, VpcReadyCondition, NetworkValidationFailedReason, "b")

	conds := c.GetConditions()
	if len(conds) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(conds))
	}
	byType := map[string]metav1.Condition{}
	for _, cond := range conds {
		byType[cond.Type] = cond
	}
	if byType[NetworkReadyCondition].Status != metav1.ConditionTrue {
		t.Errorf("NetworkReady status = %q, want True", byType[NetworkReadyCondition].Status)
	}
	if byType[VpcReadyCondition].Status != metav1.ConditionFalse {
		t.Errorf("VpcReady status = %q, want False", byType[VpcReadyCondition].Status)
	}
}
