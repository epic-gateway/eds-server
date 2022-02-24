package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
	"gitlab.com/acnodal/epic/resource-model/controllers"
)

const (
	// This server runs in each customer namespace so this is only the
	// base of the finalizer name, it needs to have the namespace
	// prepended
	sliceFinalizerNameBase = "slice.eds.epic.acnodal.io"
)

// GWEndpointSliceReconciler reconciles a GWEndpointSlice object
type GWEndpointSliceReconciler struct {
	Callbacks RouteCallbacks

	client        client.Client
	log           logr.Logger
	runtimeScheme *runtime.Scheme
}

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *GWEndpointSliceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	l := r.log.WithValues("slice", req.NamespacedName)
	nsFinalizerName := fmt.Sprintf("%s.%s", req.Namespace, sliceFinalizerNameBase)
	l.Info("reconciling")

	// read the slice that caused the event
	slice := epicv1.GWEndpointSlice{}
	if err := r.client.Get(ctx, req.NamespacedName, &slice); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	if !slice.ObjectMeta.DeletionTimestamp.IsZero() {
		// This resource is marked to be deleted. Remove our finalizer
		// before we do anything else to ensure that we don't block it
		// from being deleted.
		if err := controllers.RemoveFinalizer(ctx, r.client, &slice, nsFinalizerName); err != nil {
			return done, err
		}
	} else {
		// The resource is not being deleted, so if it does not have our
		// finalizer, then add it.
		if err := controllers.AddFinalizer(ctx, r.client, &slice, nsFinalizerName); err != nil {
			return done, err
		}
	}

	l.Info("parent", "proxy", slice.Spec.ParentRef)

	proxies, err := referencingProxies(ctx, r.client, req.Namespace, slice.Spec.ParentRef.UID)
	if err != nil {
		return done, err
	}

	l.Info("referencers", "proxies", proxies)

	// This slice can be referenced by multiple GWProxies. We need to
	// update all of them.
	for _, parent := range proxies {
		proxyName := types.NamespacedName{Namespace: req.Namespace, Name: parent}
		pl := l.WithValues("parent", proxyName)
		pl.Info("updating")

		proxy := epicv1.GWProxy{}
		if err := r.client.Get(ctx, proxyName, &proxy); err != nil {
			pl.Info("Can't get parent proxy")
		} else {
			// tell the control plane about the changed object
			if err := r.Callbacks.UpdateProxy(&proxy); err != nil {
				return done, err
			}
		}
	}

	return done, nil
}

func referencingProxies(ctx context.Context, cl client.Client, namespace string, cluster string) ([]string, error) {
	refs := []string{}

	listOps := client.ListOptions{Namespace: namespace}
	routes := epicv1.GWRouteList{}
	err := cl.List(ctx, &routes, &listOps)
	if err != nil {
		return refs, err
	}

	for _, route := range routes.Items {
		for _, rule := range route.Spec.HTTP.Rules {
			for _, ref := range rule.BackendRefs {
				backendName := string(ref.Name)
				if backendName == cluster {
					for _, parent := range route.Spec.HTTP.ParentRefs {
						refs = append(refs, string(parent.Name))
					}
				}
			}
		}
	}

	return refs, nil
}

// SetupWithManager sets up this reconciler to be managed.
func (r *GWEndpointSliceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.client = mgr.GetClient()
	r.log = ctrl.Log.WithName("GWEndpointSliceController")
	r.runtimeScheme = mgr.GetScheme()

	return ctrl.NewControllerManagedBy(mgr).
		For(&epicv1.GWEndpointSlice{}).
		Complete(r)
}

// Scheme returns this reconciler's scheme.
func (r *GWEndpointSliceReconciler) Scheme() *runtime.Scheme {
	return r.runtimeScheme
}