package controllers

import (
	"context"
	"fmt"

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
	EndpointChanged(int, *egwv1.LoadBalancer, []egwv1.RemoteEndpoint) error
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

	// Allocate a snapshot version from the LB
	version, err := allocateSnapshotVersion(ctx, r, lb)
	if err != nil {
		return done, err
	}

	l.Info("snapshot version allocated", "version", version)

	// tell the control plane about the current state of this LB (and
	// its EPs)
	if err := r.Callbacks.EndpointChanged(version, lb, eps); err != nil {
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

// allocateSnapshotVersion allocates a snapshot version that's unique
// to this process. If this call succeeds (i.e., error is nil) then
// lb.Status.ProxySnapshotVersion will be unique to this instance of
// lb.
func allocateSnapshotVersion(ctx context.Context, cl client.Client, lb *egwv1.LoadBalancer) (version int, err error) {
	tries := 3
	for err = fmt.Errorf(""); err != nil && tries > 0; tries-- {
		version, err = nextSnapshotVersion(ctx, cl, lb)
	}
	return version, err
}

// nextSnapshotVersion gets the next LB snapshot version by doing a
// read-modify-write cycle. It might be inefficient in terms of not
// using all of the values that it allocates but it's safe because the
// Update() will only succeed if the LB hasn't been modified since the
// Get().
//
// This function doesn't retry so if there's a collision with some
// other process the caller needs to retry.
func nextSnapshotVersion(ctx context.Context, cl client.Client, lb *egwv1.LoadBalancer) (version int, err error) {

	// get the SG
	sg := egwv1.ServiceGroup{}
	err = cl.Get(ctx, types.NamespacedName{Namespace: lb.Namespace, Name: lb.Labels[egwv1.OwningServiceGroupLabel]}, &sg)
	if err != nil {
		return -1, err
	}

	// Initialize this SG's map (if necessary)
	if versions := sg.Status.ProxySnapshotVersions; versions == nil {
		sg.Status.ProxySnapshotVersions = map[string]int{}
	}

	// Initialize or increment this LB's snapshot version
	version, exists := sg.Status.ProxySnapshotVersions[lb.Name]
	if !exists {
		version = 0
	} else {
		version++
	}
	sg.Status.ProxySnapshotVersions[lb.Name] = version

	return version, cl.Status().Update(ctx, &sg)
}
