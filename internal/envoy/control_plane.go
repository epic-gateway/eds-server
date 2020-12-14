package envoy

import (
	"context"

	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	testv3 "github.com/envoyproxy/go-control-plane/pkg/test/v3"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

var (
	cache cachev3.SnapshotCache
	l     Logger
)

// UpdateModel updates Envoy's model with new info about this LB.
func UpdateModel(nodeID string, service egwv1.LoadBalancer, endpoints []egwv1.Endpoint) error {
	snapshot := ServiceToSnapshot(service, endpoints)
	return updateSnapshot(nodeID, snapshot)
}

func updateSnapshot(nodeID string, snapshot cachev3.Snapshot) error {
	if err := snapshot.Consistent(); err != nil {
		l.Errorf("snapshot inconsistency: %+v\n%+v", snapshot, err)
		return err
	}
	l.Debugf("will serve snapshot %+v", snapshot)

	// add the snapshot to the cache
	if err := cache.SetSnapshot(nodeID, snapshot); err != nil {
		l.Errorf("snapshot error %q for %+v", err, snapshot)
		return err
	}

	return nil
}

// LaunchControlPlane launches an xDS control plane in the
// foreground. Note that this means that this function doesn't return.
func LaunchControlPlane(xDSPort uint, debug bool) error {
	l = Logger{Debug: debug}

	// create a cache
	cache = cachev3.NewSnapshotCache(false, cachev3.IDHash{}, l)
	cbv3 := &testv3.Callbacks{Debug: debug}
	srv3 := serverv3.NewServer(context.Background(), cache, cbv3)

	// run the xDS server
	runServer(context.Background(), srv3, xDSPort)

	return nil
}
