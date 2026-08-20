/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

// Package main is the entrypoint of the CCE Cluster API provider manager.
package main

import (
	"flag"
	"os"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	controlplanev1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/controlplane/v1beta1"
	infrav1beta1 "github.com/huaweicloud/cloudnative-cluster-api-provider-cce/api/infrastructure/v1beta1"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/controllers"
	"github.com/huaweicloud/cloudnative-cluster-api-provider-cce/internal/features"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(infrav1beta1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1beta1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		leaderElectionID     string
		featureGates         string
		validFlavors         string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "cce-provider-leader-election", "Leader election ID.")
	flag.StringVar(&featureGates, "feature-gates", "",
		"Comma-separated list of feature gate overrides, e.g. 'NodePoolAutoscaling=true'.")
	flag.StringVar(&validFlavors, "valid-flavors", "",
		"Comma-separated allowlist of ECS flavors accepted by the CCEManagedMachinePool webhook "+
			"(empty = format check only; region availability is still enforced by CCE at create time).")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := features.RegisterFeatureGates(); err != nil {
		setupLog.Error(err, "unable to register feature gates")
		os.Exit(1)
	}
	if featureGates != "" {
		overrides, err := parseFeatureGates(featureGates)
		if err != nil {
			setupLog.Error(err, "unable to parse --feature-gates")
			os.Exit(1)
		}
		if err := features.SetFromMap(overrides); err != nil {
			setupLog.Error(err, "unable to apply --feature-gates")
			os.Exit(1)
		}
	}
	if validFlavors != "" {
		infrav1beta1.ValidFlavors = splitCSV(validFlavors)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), manager.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := controllers.SetupControllers(mgr); err != nil {
		setupLog.Error(err, "unable to create controllers")
		os.Exit(1)
	}

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := (&infrav1beta1.CCECluster{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CCECluster")
			os.Exit(1)
		}
		if err := (&infrav1beta1.CCEManagedMachinePool{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CCEManagedMachinePool")
			os.Exit(1)
		}
		if err := (&controlplanev1beta1.CCEManagedControlPlane{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CCEManagedControlPlane")
			os.Exit(1)
		}
		if err := (&infrav1beta1.CCEClusterTemplate{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CCEClusterTemplate")
			os.Exit(1)
		}
		if err := (&controlplanev1beta1.CCEManagedControlPlaneTemplate{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CCEManagedControlPlaneTemplate")
			os.Exit(1)
		}
		if err := (&infrav1beta1.CCEManagedMachinePoolTemplate{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CCEManagedMachinePoolTemplate")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// parseFeatureGates parses "Name=true,Name=false" into a map.
func parseFeatureGates(s string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, item := range splitCSV(s) {
		kv := strings.SplitN(item, "=", 2)
		if len(kv) != 2 {
			return nil, errors.Errorf("invalid feature gate %q, want Name=true|false", item)
		}
		var v bool
		switch strings.ToLower(kv[1]) {
		case "true":
			v = true
		case "false":
			v = false
		default:
			return nil, errors.Errorf("invalid feature gate value %q, want true|false", kv[1])
		}
		out[kv[0]] = v
	}
	return out, nil
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
