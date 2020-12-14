// Copyright 2020 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package envoy

import (
	"time"

	"github.com/golang/protobuf/ptypes"
	v1 "k8s.io/api/core/v1"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

const (
	routeName    = "local_route"
	listenerName = "listener_0"
)

var (
	version int
)

func serviceToCluster(service egwv1.LoadBalancer, endpoints []egwv1.Endpoint) *cluster.Cluster {
	// Translate EGW endpoints into Envoy LbEndpoints
	lbEndpoints := make([]*endpoint.LbEndpoint, len(endpoints))
	for i, ep := range endpoints {
		lbEndpoints[i] = EndpointToLbEndpoint(ep)
	}

	return &cluster.Cluster{
		Name:                 service.Name,
		ConnectTimeout:       ptypes.DurationProto(5 * time.Second),
		ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_STATIC},
		DnsLookupFamily:      cluster.Cluster_V4_ONLY, // FIXME: using IPV6 I get:
		// upstream connect error or disconnect/reset before headers. reset reason: connection failure
		LbPolicy: cluster.Cluster_ROUND_ROBIN,
		LoadAssignment: &endpoint.ClusterLoadAssignment{
			ClusterName: service.Name,
			Endpoints: []*endpoint.LocalityLbEndpoints{{
				LbEndpoints: lbEndpoints,
			}},
		},
	}
}

// EndpointToLbEndpoint translates one of our
// egwv1.LoadBalancerEndpoints into one of Envoy's
// endpoint.LbEndpoints.
func EndpointToLbEndpoint(ep egwv1.Endpoint) *endpoint.LbEndpoint {
	return &endpoint.LbEndpoint{
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Protocol: protocolToProtocol(ep.Spec.Port.Protocol),
							Address:  ep.Spec.Address,
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: uint32(ep.Spec.Port.Port),
							},
						},
					},
				},
			},
		},
	}
}

func makeHTTPListener(service egwv1.LoadBalancer, listenerName string, route string, upstreamHost string) *listener.Listener {
	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.HttpConnectionManager_AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: makeRoute(routeName, service.Name, upstreamHost),
		},
		HttpFilters: []*hcm.HttpFilter{{
			Name: wellknown.Router,
		}},
	}
	pbst, err := ptypes.MarshalAny(manager)
	if err != nil {
		panic(err)
	}

	return &listener.Listener{
		Name: listenerName,
		Address: &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Protocol: protocolToProtocol(service.Spec.PublicPorts[0].Protocol),
					Address:  "0.0.0.0",
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: uint32(service.Spec.PublicPorts[0].Port),
					},
				},
			},
		},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name: wellknown.HTTPConnectionManager,
				ConfigType: &listener.Filter_TypedConfig{
					TypedConfig: pbst,
				},
			}},
		}},
	}
}

func makeRoute(routeName string, clusterName string, upstreamHost string) *route.RouteConfiguration {
	return &route.RouteConfiguration{
		Name: routeName,
		VirtualHosts: []*route.VirtualHost{{
			Name:    "local_service",
			Domains: []string{"*"},
			Routes: []*route.Route{{
				Match: &route.RouteMatch{
					PathSpecifier: &route.RouteMatch_Prefix{
						Prefix: "/",
					},
				},
				Action: &route.Route_Route{
					Route: &route.RouteAction{
						ClusterSpecifier: &route.RouteAction_Cluster{
							Cluster: clusterName,
						},
						HostRewriteSpecifier: &route.RouteAction_HostRewriteLiteral{
							HostRewriteLiteral: upstreamHost,
						},
					},
				},
			}},
		}},
	}
}

// ServiceToSnapshot translates one of our egwv1.LoadBalancers into an
// xDS cachev3.Snapshot.
func ServiceToSnapshot(service egwv1.LoadBalancer, endpoints []egwv1.Endpoint) cachev3.Snapshot {
	version++
	return cachev3.NewSnapshot(
		string(version),
		[]types.Resource{}, // endpoints
		[]types.Resource{serviceToCluster(service, endpoints)},
		[]types.Resource{}, // routes
		// FIXME: we currently need this Address because we're doing HTTP
		// rewriting which we probably don't want to do
		[]types.Resource{makeHTTPListener(service, listenerName, routeName, service.Spec.PublicAddress)},
		[]types.Resource{}, // runtimes
	)
}

// NewSnapshot returns an empty snapshot.
func NewSnapshot() cachev3.Snapshot {
	version++
	return cachev3.NewSnapshot(
		string(version),
		[]types.Resource{}, // endpoints
		[]types.Resource{}, // clusters
		[]types.Resource{}, // routes
		[]types.Resource{}, // listeners
		[]types.Resource{}, // runtimes
	)
}

// protocolToProtocol translates from k8s core Protocol objects to
// Envoy code SocketAddress_Protocol objects.
func protocolToProtocol(protocol v1.Protocol) core.SocketAddress_Protocol {
	eProto := core.SocketAddress_TCP
	if protocol == v1.ProtocolUDP {
		eProto = core.SocketAddress_UDP
	}
	return eProto
}
