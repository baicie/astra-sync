package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	synccontroller "io.astrasync/control-plane/controller/internal/controller"
)

func main() {
	var metricsAddress string
	var probeAddress string
	var leaderElection bool
	var leaseDuration time.Duration
	var renewDeadline time.Duration
	var retryPeriod time.Duration

	flag.StringVar(&metricsAddress, "metrics-bind-address", ":9090", "Metrics endpoint address")
	flag.StringVar(&probeAddress, "health-probe-bind-address", ":8081", "Health probe endpoint address")
	flag.BoolVar(&leaderElection, "leader-elect", true, "Enable Kubernetes Lease leader election")
	flag.DurationVar(&leaseDuration, "leader-elect-lease-duration", 15*time.Second, "Leader lease duration")
	flag.DurationVar(&renewDeadline, "leader-elect-renew-deadline", 10*time.Second, "Leader renewal deadline")
	flag.DurationVar(&retryPeriod, "leader-elect-retry-period", 2*time.Second, "Leader election retry period")
	loggerOptions := zap.Options{Development: false}
	loggerOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&loggerOptions)))

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(syncv1.AddToScheme(scheme))

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: metricsAddress},
		HealthProbeBindAddress:  probeAddress,
		LeaderElection:          leaderElection,
		LeaderElectionID:        "astrasync-control-plane.sync.astrasync.io",
		LeaderElectionNamespace: os.Getenv("POD_NAMESPACE"),
		LeaseDuration:           &leaseDuration,
		RenewDeadline:           &renewDeadline,
		RetryPeriod:             &retryPeriod,
	})
	if err != nil {
		ctrl.Log.Error(err, "unable to create controller manager")
		os.Exit(1)
	}

	reconciler := &synccontroller.SyncJobReconciler{
		Client: manager.GetClient(), Scheme: manager.GetScheme(), Clock: time.Now,
	}
	if err := reconciler.SetupWithManager(manager); err != nil {
		ctrl.Log.Error(err, "unable to register SyncJob controller")
		os.Exit(1)
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to register health check")
		os.Exit(1)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to register readiness check")
		os.Exit(1)
	}
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "controller manager stopped")
		os.Exit(1)
	}
}
