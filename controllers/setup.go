/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package controllers registers all provider controllers with the manager.
package controllers

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
)

// ControllerConcurrency holds the max-concurrent-reconciles settings for the
// provider controllers. Tunable via manager flags (mirrors CAPA's
// --aws-cluster-concurrency / --aws-machine-concurrency). A value of 0 means
// "use the controller-runtime default" (1, i.e. sequential).
type ControllerConcurrency struct {
	Cluster      int
	ControlPlane int
	MachinePool  int
}

// SetupControllers registers the CCECluster, CCEManagedControlPlane and
// CCEManagedMachinePool controllers, plus (when the feature gate is on) the
// CCEClusterControllerIdentity auto-creator.
func SetupControllers(mgr ctrl.Manager, concurrency ControllerConcurrency) error {
	clusterOpts := controller.Options{MaxConcurrentReconciles: concurrency.Cluster}
	cpOpts := controller.Options{MaxConcurrentReconciles: concurrency.ControlPlane}
	mpOpts := controller.Options{MaxConcurrentReconciles: concurrency.MachinePool}

	if err := (&CCEClusterReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("ccecluster-controller"),
	}).SetupWithManager(context.Background(), mgr, clusterOpts); err != nil {
		return err
	}
	if err := (&CCEManagedControlPlaneReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("ccemanagedcontrolplane-controller"),
	}).SetupWithManager(context.Background(), mgr, cpOpts); err != nil {
		return err
	}
	if err := (&CCEManagedMachinePoolReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor("ccemanagedmachinepool-controller"),
	}).SetupWithManager(context.Background(), mgr, mpOpts); err != nil {
		return err
	}
	if features.Enabled(features.AutoControllerIdentityCreator) {
		if err := (&CCEClusterControllerIdentityReconciler{Client: mgr.GetClient()}).SetupWithManager(context.Background(), mgr); err != nil {
			return err
		}
	}
	return nil
}
