package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
	"gitlab.com/acnodal/epic/resource-model/controllers"
)

const (
	// This server runs in each customer namespace so this is only the
	// base of the finalizer name, it needs to have the namespace
	// prepended
	proxyFinalizerNameBase = "proxy.eds.epic.acnodal.io"
)

// GWProxyReconciler reconciles a GWProxy object
type GWProxyReconciler struct {
	Callbacks RouteCallbacks

	client        client.Client
	log           logr.Logger
	runtimeScheme *runtime.Scheme
}

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *GWProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	l := r.log.WithValues("proxy", req.NamespacedName)
	nsFinalizerName := fmt.Sprintf("%s.%s", req.Namespace, proxyFinalizerNameBase)
	l.Info("reconciling")

	// read the proxy that caused the event
	proxy := epicv1.GWProxy{}
	if err := r.client.Get(ctx, req.NamespacedName, &proxy); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	if !proxy.ObjectMeta.DeletionTimestamp.IsZero() {
		// This LB is marked to be deleted. Remove our finalizer before we
		// do anything else to ensure that we don't block the LB CR from
		// being deleted.
		if err := controllers.RemoveFinalizer(ctx, r.client, &proxy, nsFinalizerName); err != nil {
			return done, err
		}

		// Tell the control plane that the proxy is being deleted
		r.Callbacks.DeleteNode(proxy.Namespace, proxy.Name)
		return done, nil
	}

	// The proxy is not being deleted, so if it does not have our
	// finalizer, then add it and update the object.
	if err := controllers.AddFinalizer(ctx, r.client, &proxy, nsFinalizerName); err != nil {
		return done, err
	}

	// tell the control plane about the changed object
	if err := r.Callbacks.UpdateProxy(ctx, r.client, &proxy); err != nil {
		return done, err
	}

	return done, nil
}

// SetupWithManager sets up this reconciler to be managed.
func (r *GWProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.client = mgr.GetClient()
	r.log = ctrl.Log.WithName("GWProxyController")
	r.runtimeScheme = mgr.GetScheme()

	return ctrl.NewControllerManagedBy(mgr).
		For(&epicv1.GWProxy{}).
		Complete(r)
}

// Scheme returns this reconciler's scheme.
func (r *GWProxyReconciler) Scheme() *runtime.Scheme {
	return r.runtimeScheme
}
