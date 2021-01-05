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
	"fmt"
	"strconv"
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
	routeName = "local_route"
)

func serviceToCluster(service egwv1.LoadBalancer, endpoints []egwv1.RemoteEndpoint) *cluster.Cluster {
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
func EndpointToLbEndpoint(ep egwv1.RemoteEndpoint) *endpoint.LbEndpoint {
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

// makeHTTPListeners translates an egwv1.LoadBalancer's ports into
// Envoy Listener objects.
func makeHTTPListeners(service egwv1.LoadBalancer, route string, upstreamHost string) []types.Resource {
	resources := make([]types.Resource, len(service.Spec.PublicPorts))

	for i, port := range service.Spec.PublicPorts {
		resources[i] = makeHTTPListener(service.Name, port, route, upstreamHost)
	}

	return resources
}

func makeHTTPListener(serviceName string, port v1.ServicePort, route string, upstreamHost string) *listener.Listener {
	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.HttpConnectionManager_AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: makeRoute(routeName, serviceName, upstreamHost),
		},
		HttpFilters: []*hcm.HttpFilter{{
			Name: wellknown.Router,
		}},
	}
	pbst, err := ptypes.MarshalAny(manager)
	if err != nil {
		panic(err)
	}

	// Fill in a default name if none was provided
	listenerName := port.Name
	if listenerName == "" {
		listenerName = fmt.Sprintf("%s-%d", port.Protocol, port.Port)
	}

	return &listener.Listener{
		Name: listenerName,
		Address: &core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Protocol: protocolToProtocol(port.Protocol),
					Address:  "0.0.0.0",
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: uint32(port.Port),
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
func ServiceToSnapshot(version int, service egwv1.LoadBalancer, endpoints []egwv1.RemoteEndpoint) cachev3.Snapshot {
	return cachev3.NewSnapshot(
		strconv.Itoa(version),
		[]types.Resource{}, // endpoints
		[]types.Resource{serviceToCluster(service, endpoints)},
		[]types.Resource{}, // routes
		// FIXME: we currently need this Address because we're doing HTTP
		// rewriting which we probably don't want to do
		makeHTTPListeners(service, routeName, service.Spec.PublicAddress),
		[]types.Resource{}, // runtimes
		[]types.Resource{}, // secrets
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
