package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

const (
	// name of our custom finalizer
	epFinalizerName = "remoteendpoint-finalizer.xds-controller.acnodal.io"
)

// LoadBalancerCallbacks are how this controller notifies the control
// plane of object changes.
type LoadBalancerCallbacks interface {
	EndpointChanged(*egwv1.LoadBalancer, []egwv1.RemoteEndpoint) error
	LoadBalancerDeleted(string, string)
}

// RemoteEndpointReconciler reconciles a Endpoint object
type RemoteEndpointReconciler struct {
	client.Client
	Log       logr.Logger
	Callbacks LoadBalancerCallbacks
	Scheme    *runtime.Scheme
}

// +kubebuilder:rbac:groups=egw.acnodal.io,resources=endpoints,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=egw.acnodal.io,resources=endpoints/status,verbs=get;update;patch

// +kubebuilder:rbac:groups=egw.acnodal.io,resources=loadbalancers/status,verbs=get;update;patch

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *RemoteEndpointReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	ctx := context.TODO()
	l := r.Log.WithValues("endpoint", req.NamespacedName)

	l.Info("reconciling")

	// get the object that caused the event
	rep := &egwv1.RemoteEndpoint{}
	if err := r.Get(ctx, req.NamespacedName, rep); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	if !rep.ObjectMeta.DeletionTimestamp.IsZero() {
		// This endpoint is marked to be deleted. Remove our finalizer
		// before we do anything else to ensure that we don't block the
		// endpoint CR from being deleted.
		if controllerutil.ContainsFinalizer(rep, epFinalizerName) {
			// remove our finalizer from the list and update the object
			controllerutil.RemoveFinalizer(rep, epFinalizerName)
			if err := r.Update(ctx, rep); err != nil {
				return done, err
			}
		}
	} else {
		// The object is not being deleted, so if it does not have our
		// finalizer, then add it and update the object.
		if !controllerutil.ContainsFinalizer(rep, epFinalizerName) {
			controllerutil.AddFinalizer(rep, epFinalizerName)
			if err := r.Update(ctx, rep); err != nil {
				return done, err
			}
		}
	}

	// get the parent LB
	lb := &egwv1.LoadBalancer{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: rep.Namespace, Name: rep.Labels[egwv1.OwningLoadBalancerLabel]}, lb); err != nil {
		return done, err
	}

	// list the endpoints that belong to the parent LB
	eps, err := listActiveLBEndpoints(r, lb)
	if err != nil {
		return done, err
	}

	if rep.ObjectMeta.DeletionTimestamp.IsZero() {
	}

	// tell the control plane about the current state of this LB (and
	// its EPs)
	if err := r.Callbacks.EndpointChanged(lb, eps); err != nil {
		return done, err
	}

	return done, nil
}

// SetupWithManager sets up this reconciler to be managed.
func (r *RemoteEndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&egwv1.RemoteEndpoint{}).
		Complete(r)
}
