package envoy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	server "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/test/v3"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
)

const (
	tlsServerCertificatePath = "/etc/envoy/tls/server"
	tlsCACertificatePath     = "/etc/envoy/tls/ca"
	tlsCertificateFile       = "tls.crt"
	tlsCertificateKeyFile    = "tls.key"
)

var (
	snapshotCache cache.SnapshotCache
	l             Logger
	c             client.Client

	// Allocating new snapshot versions and updating the cache is a
	// critical region. We want to ensure that the version only
	// increases so we don't want to have a call to UpdateModel() be
	// interrupted in the middle by another call to UpdateModel()
	// because that could cause the version to decrease.
	updateLock sync.Mutex
)

// UpdateModel updates Envoy's model with new info about this LB.
func UpdateModel(nodeID string, service *epicv1.LoadBalancer, endpoints []epicv1.RemoteEndpoint) error {
	defer updateLock.Unlock()
	updateLock.Lock()

	version, err := allocateSnapshotVersion(context.TODO(), c, service.Namespace, service.Labels[epicv1.OwningLBServiceGroupLabel], service.Name)
	if err != nil {
		return err
	}

	snapshot, err := RepsToSnapshot(version, service.Spec.EnvoyTemplate.EnvoyResources.Endpoints[0].Value, endpoints)
	if err != nil {
		return err
	}
	return updateSnapshot(nodeID, snapshot)
}

// UpdateProxyModel updates Envoy's model with new info about this GWProxy.
func UpdateProxyModel(nodeID string, proxy *epicv1.GWProxy) error {
	// FIXME: we should probably have the controllers provide the
	// context, and maybe the client.
	var ctx = context.TODO()

	defer updateLock.Unlock()
	updateLock.Lock()

	version, err := allocateSnapshotVersion(ctx, c, proxy.Namespace, proxy.Labels[epicv1.OwningLBServiceGroupLabel], proxy.Name)
	if err != nil {
		return err
	}

	endpoints, err := activeProxyEndpoints(ctx, c, proxy)
	snapshot, err := RepsToSnapshot(version, proxy.Spec.EnvoyTemplate.EnvoyResources.Endpoints[0].Value, endpoints)
	if err != nil {
		return err
	}

	return updateSnapshot(nodeID, snapshot)
}

func updateSnapshot(nodeID string, snapshot cache.Snapshot) error {
	l.Debugf("will serve snapshot %#v", snapshot)

	// add the snapshot to the cache
	if err := snapshotCache.SetSnapshot(nodeID, snapshot); err != nil {
		l.Errorf("snapshot error %q for %+v", err, snapshot)
		return err
	}

	return nil
}

// activeProxyEndpoints lists endpoints that belong to the proxy and
// that are active, i.e., not in the process of being deleted.
func activeProxyEndpoints(ctx context.Context, cl client.Client, proxy *epicv1.GWProxy) ([]epicv1.RemoteEndpoint, error) {
	activeEPs := []epicv1.RemoteEndpoint{}
	listOps := client.ListOptions{Namespace: proxy.Namespace}
	routes := epicv1.GWRouteList{}
	err := cl.List(ctx, &routes, &listOps)
	if err != nil {
		return activeEPs, err
	}
	slices := epicv1.GWEndpointSliceList{}
	err = cl.List(ctx, &slices, &listOps)
	if err != nil {
		return activeEPs, err
	}

	for _, route := range routes.Items {
		for _, rule := range route.Spec.HTTP.Rules {
			for _, ref := range rule.BackendRefs {
				clusterName := string(ref.Name)
				for _, slice := range slices.Items {
					if slice.Spec.ParentRef.UID == clusterName && slice.ObjectMeta.DeletionTimestamp.IsZero() {
						for _, endpoint := range slice.Spec.Endpoints {
							for _, address := range endpoint.Addresses {
								activeEPs = append(activeEPs, epicv1.RemoteEndpoint{
									Spec: epicv1.RemoteEndpointSpec{
										Cluster: clusterName,
										Address: address,
										Port: v1.EndpointPort{
											Port:     *slice.Spec.Ports[0].Port,
											Protocol: *slice.Spec.Ports[0].Protocol,
										},
									},
								})
							}
						}
					}
				}
			}
		}
	}

	return activeEPs, err
}

// ClearModel removes a model from the cache.
func ClearModel(nodeID string) {
	snapshotCache.ClearSnapshot(nodeID)
}

// LaunchControlPlane launches an xDS control plane in the
// foreground. Note that this means that this function doesn't return.
func LaunchControlPlane(client client.Client, log logr.Logger, xDSPort uint, debug bool) error {
	l = Logger{Debug: debug}
	c = client

	// create a cache
	snapshotCache = cache.NewSnapshotCache(false, cache.IDHash{}, l)
	cb := &test.Callbacks{Debug: debug}
	srvr := server.NewServer(context.Background(), snapshotCache, cb)

	// run the xDS server
	runServer(context.Background(),
		srvr,
		xDSPort,
		&tls.Config{
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				// Sadly, these 2 non 256 are required to use http2 in go
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			},
			Certificates: []tls.Certificate{loadCertificate(tlsServerCertificatePath, log)},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    loadCA(tlsCACertificatePath, log),
		})

	return nil
}

// allocateSnapshotVersion allocates a snapshot version that's unique
// to this process. If this call succeeds (i.e., error is nil) then
// lb.Status.ProxySnapshotVersion will be unique to this instance of
// lb.
func allocateSnapshotVersion(ctx context.Context, cl client.Client, ns string, lbsgName string, lbName string) (version int, err error) {
	tries := 3
	for err = fmt.Errorf(""); err != nil && tries > 0; tries-- {
		// get the SG
		sg := epicv1.LBServiceGroup{}
		err = cl.Get(ctx, types.NamespacedName{Namespace: ns, Name: lbsgName}, &sg)
		if err != nil {
			return -1, err
		}

		version, err = nextSnapshotVersion(ctx, cl, sg, lbName)
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
func nextSnapshotVersion(ctx context.Context, cl client.Client, sg epicv1.LBServiceGroup, lb string) (version int, err error) {

	// Initialize this SG's map (if necessary)
	if versions := sg.Status.ProxySnapshotVersions; versions == nil {
		sg.Status.ProxySnapshotVersions = map[string]int{}
	}

	// Initialize or increment this LB's snapshot version
	version, exists := sg.Status.ProxySnapshotVersions[lb]
	if !exists {
		version = 0
	} else {
		version++
	}
	sg.Status.ProxySnapshotVersions[lb] = version

	return version, cl.Status().Update(ctx, &sg)
}

func loadCertificate(directory string, logger logr.Logger) tls.Certificate {
	certificate, err := tls.LoadX509KeyPair(
		fmt.Sprintf("%s/%s", directory, tlsCertificateFile),
		fmt.Sprintf("%s/%s", directory, tlsCertificateKeyFile),
	)
	if err != nil {
		logger.Error(err, "Could not load server certificate")
		os.Exit(1)
	}
	return certificate
}

func loadCA(directory string, logger logr.Logger) *x509.CertPool {
	certPool := x509.NewCertPool()
	if bs, err := ioutil.ReadFile(fmt.Sprintf("%s/%s", directory, tlsCertificateFile)); err != nil {
		logger.Error(err, "Failed to read client ca cert")
		os.Exit(1)
	} else {
		ok := certPool.AppendCertsFromPEM(bs)
		if !ok {
			logger.Error(err, "Failed to append client certs")
			os.Exit(1)
		}
	}
	return certPool
}
