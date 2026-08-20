/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// recordEvent emits a Kubernetes event through rec, if rec is non-nil.
// Reconcilers built directly in tests do not set a recorder, so the nil guard
// keeps those paths working without requiring a fake recorder everywhere.
func recordEvent(rec record.EventRecorder, obj runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	if rec != nil {
		rec.Eventf(obj, eventtype, reason, messageFmt, args...)
	}
}
