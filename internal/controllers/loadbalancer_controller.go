package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
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
	finalizerNameBase = "lb.eds.epic.acnodal.io"
)

// LoadBalancerReconciler reconciles a LoadBalancer object
type LoadBalancerReconciler struct {
	client.Client
	Log           logr.Logger
	Callbacks     LoadBalancerCallbacks
	RuntimeScheme *runtime.Scheme
}

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *LoadBalancerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	l := r.Log.WithValues("loadbalancer", req.NamespacedName)
	nsFinalizerName := fmt.Sprintf("%s.%s", req.Namespace, finalizerNameBase)
	l.Info("reconciling")

	// read the LB that caused the event
	lb := &epicv1.LoadBalancer{}
	if err := r.Get(ctx, req.NamespacedName, lb); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	if !lb.ObjectMeta.DeletionTimestamp.IsZero() {
		// This LB is marked to be deleted. Remove our finalizer before we
		// do anything else to ensure that we don't block the LB CR from
		// being deleted.
		if err := controllers.RemoveFinalizer(ctx, r.Client, lb, nsFinalizerName); err != nil {
			return done, err
		}

		// Tell the control plane that the LB is being deleted
		r.Callbacks.DeleteNode(lb.Namespace, lb.Name)
		return done, nil
	}

	// The LB is not being deleted, so if it does not have our
	// finalizer, then add it and update the object.
	if err := controllers.AddFinalizer(ctx, r.Client, lb, nsFinalizerName); err != nil {
		return done, err
	}

	endpoints, err := listActiveLBEndpoints(r, lb)
	if err != nil {
		return done, err
	}

	// tell the control plane about the changed object
	if err := r.Callbacks.EndpointChanged(lb, endpoints); err != nil {
		return done, err
	}

	return done, nil
}

// listActiveLBEndpoints lists the endpoints that belong to lb that
// are active, i.e., not in the process of being deleted.
func listActiveLBEndpoints(cl client.Client, lb *epicv1.LoadBalancer) ([]epicv1.RemoteEndpoint, error) {
	labelSelector := labels.SelectorFromSet(map[string]string{epicv1.OwningLoadBalancerLabel: lb.Name})
	listOps := client.ListOptions{Namespace: lb.Namespace, LabelSelector: labelSelector}
	list := epicv1.RemoteEndpointList{}
	err := cl.List(context.TODO(), &list, &listOps)

	activeEPs := []epicv1.RemoteEndpoint{}
	// build a new list with no "in deletion" endpoints
	for _, endpoint := range list.Items {
		if endpoint.ObjectMeta.DeletionTimestamp.IsZero() {
			activeEPs = append(activeEPs, endpoint)
		}
	}

	return activeEPs, err
}

// SetupWithManager sets up this reconciler to be managed.
func (r *LoadBalancerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&epicv1.LoadBalancer{}).
		Complete(r)
}

// Scheme returns this reconciler's scheme.
func (r *LoadBalancerReconciler) Scheme() *runtime.Scheme {
	return r.RuntimeScheme
}
