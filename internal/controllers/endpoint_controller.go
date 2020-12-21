package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

const (
	// name of our custom finalizer
	epFinalizerName = "endpoint-finalizer.xds-controller.acnodal.io"
)

// LoadBalancerCallbacks are how this controller notifies the control
// plane of object changes.
type LoadBalancerCallbacks interface {
	EndpointChanged(int, *egwv1.LoadBalancer, []egwv1.Endpoint) error
	LoadBalancerDeleted(string, string)
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

// +kubebuilder:rbac:groups=egw.acnodal.io,resources=loadbalancers/status,verbs=get;update;patch

// Reconcile is the core of this controller. It gets requests from the
// controller-runtime and figures out what to do with them.
func (r *EndpointReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	done := ctrl.Result{Requeue: false}
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
		if containsString(ep.ObjectMeta.Finalizers, epFinalizerName) {
			l.Info("removing finalizer to allow delete to proceed")

			// remove our finalizer from the list and update it.
			ep.ObjectMeta.Finalizers = removeString(ep.ObjectMeta.Finalizers, epFinalizerName)
			if err := r.Update(context.Background(), ep); err != nil {
				return done, err
			}
		}
	}

	// get the parent LB
	lb := &egwv1.LoadBalancer{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ep.Namespace, Name: ep.Spec.LoadBalancer}, lb); err != nil {
		return done, err
	}

	// list the endpoints that belong to the parent LB
	eps, err := listActiveLBEndpoints(r, lb)
	if err != nil {
		return done, err
	}

	if ep.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our
		// finalizer, then lets add the finalizer and update the
		// object. This is equivalent to registering our finalizer.
		if !containsString(ep.ObjectMeta.Finalizers, epFinalizerName) {
			ep.ObjectMeta.Finalizers = append(ep.ObjectMeta.Finalizers, epFinalizerName)
			if err := r.Update(context.Background(), ep); err != nil {
				return ctrl.Result{}, err
			}
		}
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
func (r *EndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&egwv1.Endpoint{}).
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
	err = cl.Get(ctx, types.NamespacedName{Namespace: lb.Namespace, Name: lb.Spec.ServiceGroup}, &sg)
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
