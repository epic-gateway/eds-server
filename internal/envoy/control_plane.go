package envoy

import (
	"context"
	"fmt"
	"sync"

	cachev2 "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	serverv2 "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	testv2 "github.com/envoyproxy/go-control-plane/pkg/test/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

var (
	cache cachev2.SnapshotCache
	l     Logger
	c     client.Client

	// Allocating new cache versions and updating the cache is a
	// critical region. We want to ensure that the version only
	// increases so we don't want to have a call to UpdateModel() be
	// interrupted in the middle by another call to UpdateModel()
	// because that could cause the version to decrease.
	updateLock sync.Mutex
)

// UpdateModel updates Envoy's model with new info about this LB.
func UpdateModel(nodeID string, service *egwv1.LoadBalancer, endpoints []egwv1.RemoteEndpoint) error {
	defer updateLock.Unlock()
	updateLock.Lock()

	version, err := allocateSnapshotVersion(context.TODO(), c, service)
	if err != nil {
		return err
	}

	snapshot, err := ServiceToSnapshot(version, service, endpoints)
	if err != nil {
		return err
	}
	return updateSnapshot(nodeID, snapshot)
}

func updateSnapshot(nodeID string, snapshot cachev2.Snapshot) error {
	l.Debugf("will serve snapshot %#v", snapshot)

	// add the snapshot to the cache
	if err := cache.SetSnapshot(nodeID, snapshot); err != nil {
		l.Errorf("snapshot error %q for %+v", err, snapshot)
		return err
	}

	return nil
}

// ClearModel removes a model from the cache.
func ClearModel(nodeID string) {
	cache.ClearSnapshot(nodeID)
}

// LaunchControlPlane launches an xDS control plane in the
// foreground. Note that this means that this function doesn't return.
func LaunchControlPlane(client client.Client, xDSPort uint, debug bool) error {
	l = Logger{Debug: debug}
	c = client

	// create a cache
	cache = cachev2.NewSnapshotCache(false, cachev2.IDHash{}, l)
	cbv2 := &testv2.Callbacks{Debug: debug}
	srv2 := serverv2.NewServer(context.Background(), cache, cbv2)

	// run the xDS server
	runServer(context.Background(), srv2, xDSPort)

	return nil
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
