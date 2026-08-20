/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package controllers registers all provider controllers with the manager.
package controllers

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
)

// SetupControllers registers the CCECluster, CCEManagedControlPlane and
// CCEManagedMachinePool controllers, plus (when the feature gate is on) the
// CCEClusterControllerIdentity auto-creator.
func SetupControllers(mgr ctrl.Manager) error {
	if err := (&CCEClusterReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("ccecluster-controller"),
	}).SetupWithManager(context.Background(), mgr); err != nil {
		return err
	}
	if err := (&CCEManagedControlPlaneReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("ccemanagedcontrolplane-controller"),
	}).SetupWithManager(context.Background(), mgr); err != nil {
		return err
	}
	if err := (&CCEManagedMachinePoolReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("ccemanagedmachinepool-controller"),
	}).SetupWithManager(context.Background(), mgr); err != nil {
		return err
	}
	if features.Enabled(features.AutoControllerIdentityCreator) {
		if err := (&CCEClusterControllerIdentityReconciler{Client: mgr.GetClient()}).SetupWithManager(context.Background(), mgr); err != nil {
			return err
		}
	}
	return nil
}
