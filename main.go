package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"acnodal.io/epic/eds-server/internal/controllers"
	"acnodal.io/epic/eds-server/internal/envoy"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
	// +kubebuilder:scaffold:imports
)

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which specifies the Namespace to watch.
	// An empty value means the operator is running with cluster scope.
	watchNamespaceEnvVar string = "WATCH_NAMESPACE"
	certificateFile      string = "tls.crt"
	certificateKeyFile   string = "tls.key"
)

var (
	scheme                       = runtime.NewScheme()
	setupLog                     = ctrl.Log.WithName("setup")
	xdssTLSServerCertificatePath string
	xdssTLSCACertificatePath     string
	xDSDebug                     bool
	xDSPort                      uint
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(epicv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type callbacks struct {
}

func (cb callbacks) DeleteNode(namespace string, name string) {
	nodeID := namespace + "." + name
	envoy.ClearModel(nodeID)
}

func (cb callbacks) UpdateProxy(ctx context.Context, cl client.Client, proxy *epicv1.GWProxy) error {
	nodeID := proxy.Namespace + "." + proxy.Name
	if err := envoy.UpdateProxyModel(ctx, cl, nodeID, proxy); err != nil {
		setupLog.Error(err, "update model failed")
	}
	return nil
}

func (cb callbacks) EndpointChanged(service *epicv1.LoadBalancer, endpoints []epicv1.RemoteEndpoint) error {
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
	flag.BoolVar(&xDSDebug, "debug", true, "Enable xds debug logging")
	flag.UintVar(&xDSPort, "xds-port", 18000, "xDS management server port")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(xDSDebug)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Namespace:          env.GetString(watchNamespaceEnvVar, ""),
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

	// Loadbalancer controllers.
	if err = (&controllers.LoadBalancerReconciler{
		Client:        mgr.GetClient(),
		Callbacks:     callbacks{},
		RuntimeScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoadBalancer")
		os.Exit(1)
	}
	if err = (&controllers.RemoteEndpointReconciler{
		Client:        mgr.GetClient(),
		Callbacks:     callbacks{},
		RuntimeScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Endpoint")
		os.Exit(1)
	}

	// Gateway controllers.
	if err = (&controllers.GWProxyReconciler{
		Callbacks: callbacks{},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GWProxy")
		os.Exit(1)
	}
	if err = (&controllers.GWRouteReconciler{
		Callbacks: callbacks{},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GWRoute")
		os.Exit(1)
	}
	if err = (&controllers.GWEndpointSliceReconciler{
		Callbacks: callbacks{},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GWEndpointSlice")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// launch the Envoy xDS control plane in the background
	setupLog.Info("starting xDS control plane")
	go envoy.LaunchControlPlane(mgr.GetClient(), ctrl.Log.WithName("xds"), xDSPort, xDSDebug)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	setupLog.Info("manager returned")
}
