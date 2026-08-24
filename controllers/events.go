/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// recordEvent emits a Kubernetes event through rec, if rec is non-nil.
// Reconcilers built directly in tests do not set a recorder, so the nil guard
// keeps those paths working without requiring a fake recorder everywhere.
func recordEvent(rec record.EventRecorder, obj runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	message := fmt.Sprintf(messageFmt, args...)
	if rec != nil {
		rec.Eventf(obj, eventtype, reason, "%s", message)
	}
	// Audit trail: mirror the operation into the structured log so provider
	// actions remain traceable after the Kubernetes event is garbage-collected.
	log := ctrl.Log
	if o, ok := obj.(client.Object); ok {
		log = log.WithValues("resource", client.ObjectKeyFromObject(o))
	}
	log.Info("provider operation", "eventtype", eventtype, "reason", reason, "message", message)
}
