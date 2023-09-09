package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	epicv1 "epic-gateway.org/resource-model/api/v1"
	"epic-gateway.org/resource-model/controllers"
)

const (
	// This server runs in each customer namespace so this is only the
	// base of the finalizer name, it needs to have the namespace
	// prepended
	routeFinalizerNameBase = "rt.eds.epic.acnodal.io"
)

// GWRouteReconciler reconciles a GWRoute object
type GWRouteReconciler struct {
	Callbacks RouteCallbacks

	client        client.Client
	runtimeScheme *runtime.Scheme
}

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *GWRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	l := log.FromContext(ctx)
	nsFinalizerName := fmt.Sprintf("%s.%s", req.Namespace, routeFinalizerNameBase)
	l.Info("reconciling")

	// read the route that caused the event
	route := epicv1.GWRoute{}
	if err := r.client.Get(ctx, req.NamespacedName, &route); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	if !route.ObjectMeta.DeletionTimestamp.IsZero() {
		// This resource is marked to be deleted. Remove our finalizer
		// before we do anything else to ensure that we don't block it
		// from being deleted.
		if err := controllers.RemoveFinalizer(ctx, r.client, &route, nsFinalizerName); err != nil {
			return done, err
		}
	} else {
		// The resource is not being deleted, so if it does not have our
		// finalizer, then add it.
		if err := controllers.AddFinalizer(ctx, r.client, &route, nsFinalizerName); err != nil {
			return done, err
		}
	}

	// This route can reference multiple GWProxies. Update each of them.
	for _, parent := range route.Parents() {
		proxyName := types.NamespacedName{Namespace: route.Namespace, Name: string(parent.Name)}
		pl := l.WithValues("parent", proxyName)
		pl.Info("updating")

		proxy := epicv1.GWProxy{}
		if err := r.client.Get(ctx, proxyName, &proxy); err != nil {
			pl.Info("Can't get parent proxy")
		} else {
			// tell the control plane about the changed object
			if err := r.Callbacks.UpdateProxy(ctx, r.client, &proxy); err != nil {
				return done, err
			}
		}
	}

	return done, nil
}

// SetupWithManager sets up this reconciler to be managed.
func (r *GWRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.client = mgr.GetClient()
	r.runtimeScheme = mgr.GetScheme()

	return ctrl.NewControllerManagedBy(mgr).
		For(&epicv1.GWRoute{}).
		Complete(r)
}

// Scheme returns this reconciler's scheme.
func (r *GWRouteReconciler) Scheme() *runtime.Scheme {
	return r.runtimeScheme
}
