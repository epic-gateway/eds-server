package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

const (
	// name of our custom finalizer
	myFinalizerName = "endpoint-finalizer.xds-controller.acnodal.io"
)

// LoadBalancerCallbacks are how this controller notifies the control
// plane of object changes.
type LoadBalancerCallbacks interface {
	EndpointChanged(*egwv1.LoadBalancer, []egwv1.Endpoint) error
}

// EndpointReconciler reconciles a Endpoint object
type EndpointReconciler struct {
	client.Client
	Log       logr.Logger
	Callbacks LoadBalancerCallbacks
	Scheme    *runtime.Scheme
}

// +kubebuilder:rbac:groups=egw.acnodal.io,resources=endpoints,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=egw.acnodal.io,resources=endpoints/status,verbs=get;update;patch

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *EndpointReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	result := ctrl.Result{}
	ctx := context.TODO()
	l := r.Log.WithValues("endpoint", req.NamespacedName)

	// get the object that caused the event
	ep := &egwv1.Endpoint{}
	if err := r.Get(ctx, req.NamespacedName, ep); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	// This endpoint is marked to be deleted. Remove our finalizer
	// before we do anything else to ensure that we don't block the
	// endpoint from being deleted.
	if !ep.ObjectMeta.DeletionTimestamp.IsZero() {
		if containsString(ep.ObjectMeta.Finalizers, myFinalizerName) {
			l.Info("removing finalizer to allow delete to proceed")

			// remove our finalizer from the list and update it.
			ep.ObjectMeta.Finalizers = removeString(ep.ObjectMeta.Finalizers, myFinalizerName)
			if err := r.Update(context.Background(), ep); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// get the parent LB
	lb := &egwv1.LoadBalancer{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ep.Namespace, Name: ep.Spec.LoadBalancer}, lb); err != nil {
		return result, err
	}

	// list the endpoints that belong to the parent LB
	list := egwv1.EndpointList{}
	if err := r.List(context.TODO(), &list, client.InNamespace(ep.Namespace)); err != nil {
		l.Error(err, "Listing endpoints", "name", lb.Name)
		return result, err
	}
	eps := list.Items

	if ep.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our
		// finalizer, then lets add the finalizer and update the
		// object. This is equivalent to registering our finalizer.
		if !containsString(ep.ObjectMeta.Finalizers, myFinalizerName) {
			ep.ObjectMeta.Finalizers = append(ep.ObjectMeta.Finalizers, myFinalizerName)
			if err := r.Update(context.Background(), ep); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted. remove this endpoint from the list
		// of endpoints (which will cause it to be removed from the Envoy
		// cluster)
		for i, endpoint := range eps {
			if endpoint.Name == ep.Name {
				eps = append(eps[:i], eps[i+1:]...)
			}
		}
	}

	// tell the control plane about the current state of this LB (and
	// its EPs)
	if err := r.Callbacks.EndpointChanged(lb, eps); err != nil {
		return result, err
	}

	return result, nil
}

// SetupWithManager sets up this reconciler to be managed.
func (r *EndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&egwv1.Endpoint{}).
		Complete(r)
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
