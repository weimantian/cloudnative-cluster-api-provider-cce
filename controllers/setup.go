/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package controllers registers all provider controllers with the manager.
package controllers

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

// SetupControllers registers the CCECluster, CCEManagedControlPlane and
// CCEManagedMachinePool controllers.
func SetupControllers(mgr ctrl.Manager) error {
	if err := (&CCEClusterReconciler{Client: mgr.GetClient()}).SetupWithManager(context.Background(), mgr); err != nil {
		return err
	}
	if err := (&CCEManagedControlPlaneReconciler{Client: mgr.GetClient()}).SetupWithManager(context.Background(), mgr); err != nil {
		return err
	}
	if err := (&CCEManagedMachinePoolReconciler{Client: mgr.GetClient()}).SetupWithManager(context.Background(), mgr); err != nil {
		return err
	}
	return nil
}
