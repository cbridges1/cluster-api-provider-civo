// Command manager runs the Civo Cluster API provider.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
	"github.com/cbridges1/cluster-api-provider-civo/internal/controller"
)

func main() {
	var leaderElect bool
	var probeAddr, metricsAddr string
	flag.BoolVar(&leaderElect, "leader-elect", false, "elect a leader (for more than one replica)")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe address")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "metrics address (0 disables)")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	lg := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "cluster-api-provider-civo",
	})
	if err != nil {
		lg.Error(err, "manager")
		os.Exit(1)
	}
	if err := (&controller.CivoClusterReconciler{Client: mgr.GetClient(), NewAPI: civo.NewAPI}).SetupWithManager(mgr); err != nil {
		lg.Error(err, "CivoCluster controller")
		os.Exit(1)
	}
	if err := (&controller.CivoControlPlaneReconciler{Client: mgr.GetClient(), NewAPI: civo.NewAPI}).SetupWithManager(mgr); err != nil {
		lg.Error(err, "CivoControlPlane controller")
		os.Exit(1)
	}
	_ = mgr.AddHealthzCheck("ping", healthz.Ping)
	_ = mgr.AddReadyzCheck("ping", healthz.Ping)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		lg.Error(err, "manager stopped")
		os.Exit(1)
	}
}
