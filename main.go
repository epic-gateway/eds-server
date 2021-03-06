package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"acnodal.io/epic-eds/internal/controllers"
	"acnodal.io/epic-eds/internal/envoy"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	xDSDebug bool
	xDSPort  uint
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(egwv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type callbacks struct {
}

func (cb callbacks) LoadBalancerDeleted(namespace string, LBName string) {
	nodeID := namespace + "." + LBName
	envoy.ClearModel(nodeID)
}

func (cb callbacks) EndpointChanged(service *egwv1.LoadBalancer, endpoints []egwv1.RemoteEndpoint) error {
	nodeID := service.Namespace + "." + service.Name
	setupLog.Info("nodeID version changed", "nodeid", nodeID, "service", service, "endpoints", endpoints)
	if err := envoy.UpdateModel(nodeID, service, endpoints); err != nil {
		setupLog.Error(err, "update model failed")
	}
	return nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&xDSDebug, "debug", false, "Enable xds debug logging")
	flag.UintVar(&xDSPort, "xds-port", 18000, "xDS management server port")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "1cb3972f.acnodal.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.LoadBalancerReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("LoadBalancer"),
		Callbacks: callbacks{},
		Scheme:    mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
		os.Exit(1)
	}
	if err = (&controllers.RemoteEndpointReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("Endpoint"),
		Callbacks: callbacks{},
		Scheme:    mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Endpoint")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// launch the Envoy xDS control plane in the background
	setupLog.Info("starting xDS control plane")
	go envoy.LaunchControlPlane(mgr.GetClient(), xDSPort, xDSDebug)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	setupLog.Info("manager returned")
}
