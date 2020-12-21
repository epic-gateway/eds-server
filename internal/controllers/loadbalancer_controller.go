package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

const (
	// name of our custom finalizer
	lbFinalizerName = "loadbalancer-finalizer.xds-controller.acnodal.io"
)

// LoadBalancerReconciler reconciles a LoadBalancer object
type LoadBalancerReconciler struct {
	client.Client
	Log       logr.Logger
	Callbacks LoadBalancerCallbacks
	Scheme    *runtime.Scheme
}

// +kubebuilder:rbac:groups=egw.acnodal.io,resources=loadbalancers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=egw.acnodal.io,resources=loadbalancers/status,verbs=get;update;patch

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *LoadBalancerReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
	ctx := context.TODO()
	l := r.Log.WithValues("loadbalancer", req.NamespacedName)

	// read the LB that caused the event
	lb := &egwv1.LoadBalancer{}
	if err := r.Get(ctx, req.NamespacedName, lb); err != nil {
		l.Info("can't get resource, probably deleted")
		// ignore not-found errors, since they can't be fixed by an
		// immediate requeue (we'll need to wait for a new notification),
		// and we can get them on deleted requests.
		return done, client.IgnoreNotFound(err)
	}

	if lb.ObjectMeta.DeletionTimestamp.IsZero() {
		// The LB is not being deleted, so if it does not have our
		// finalizer, then add the finalizer and update the object.
		if !containsString(lb.ObjectMeta.Finalizers, lbFinalizerName) {
			lb.ObjectMeta.Finalizers = append(lb.ObjectMeta.Finalizers, lbFinalizerName)
			if err := r.Update(context.Background(), lb); err != nil {
				return done, err
			}
		}
	} else {
		// This LB is marked to be deleted. Remove our finalizer before we
		// do anything else to ensure that we don't block the LB CR from
		// being deleted.
		if containsString(lb.ObjectMeta.Finalizers, lbFinalizerName) {
			l.Info("removing finalizer to allow delete to proceed")

			// remove our finalizer from the list and update it.
			lb.ObjectMeta.Finalizers = removeString(lb.ObjectMeta.Finalizers, lbFinalizerName)
			if err := r.Update(context.Background(), lb); err != nil {
				return done, err
			}
		}

		// Tell the control plane that the LB is being deleted
		r.Callbacks.LoadBalancerDeleted(lb.Namespace, lb.Name)
		return done, nil
	}

	endpoints, err := listActiveLBEndpoints(r, lb)
	if err != nil {
		return done, err
	}

	// Allocate a snapshot version from the LB
	version, err := allocateSnapshotVersion(ctx, r, lb)
	if err != nil {
		return done, err
	}
	l.Info("snapshot version allocated", "version", version)

	// tell the control plane about the changed object
	if err := r.Callbacks.EndpointChanged(version, lb, endpoints); err != nil {
		return done, err
	}

	return done, nil
}

// listActiveLBEndpoints lists the endpoints that belong to lb that
// are active, i.e., not in the process of being deleted.
func listActiveLBEndpoints(cl client.Client, lb *egwv1.LoadBalancer) ([]egwv1.Endpoint, error) {
	labelSelector := labels.SelectorFromSet(map[string]string{egwv1.OwningLoadBalancerLabel: lb.Name})
	listOps := client.ListOptions{Namespace: lb.Namespace, LabelSelector: labelSelector}
	list := egwv1.EndpointList{}
	err := cl.List(context.TODO(), &list, &listOps)

	activeEPs := []egwv1.Endpoint{}
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
		For(&egwv1.LoadBalancer{}).
		Complete(r)
}
