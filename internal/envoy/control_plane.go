package envoy

import (
	"context"

	cachev2 "github.com/envoyproxy/go-control-plane/pkg/cache/v2"
	serverv2 "github.com/envoyproxy/go-control-plane/pkg/server/v2"
	testv2 "github.com/envoyproxy/go-control-plane/pkg/test/v2"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

var (
	cache cachev2.SnapshotCache
	l     Logger
)

// UpdateModel updates Envoy's model with new info about this LB.
func UpdateModel(version int, nodeID string, service egwv1.LoadBalancer, endpoints []egwv1.RemoteEndpoint) error {
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
func LaunchControlPlane(xDSPort uint, debug bool) error {
	l = Logger{Debug: debug}

	// create a cache
	cache = cachev2.NewSnapshotCache(false, cachev2.IDHash{}, l)
	cbv2 := &testv2.Callbacks{Debug: debug}
	srv2 := serverv2.NewServer(context.Background(), cache, cbv2)

	// run the xDS server
	runServer(context.Background(), srv2, xDSPort)

	return nil
}
