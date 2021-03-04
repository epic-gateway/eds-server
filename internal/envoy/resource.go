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

	v1 "k8s.io/api/core/v1"

	cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev2 "github.com/envoyproxy/go-control-plane/pkg/cache/v2"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

func serviceToCLA(service egwv1.LoadBalancer, reps []egwv1.RemoteEndpoint) *cluster.ClusterLoadAssignment {
	fmt.Printf("\n\n\nprocessing reps\n")
	// Translate EGW RemoteEndpoints into Envoy LocalityLbEndpoints
	endpoints := make([]*endpoint.LocalityLbEndpoints, len(reps))
	for i, ep := range reps {
		fmt.Printf("processing rep %s/%s\n", ep.Namespace, ep.Name)
		endpoints[i] = repToEndpoint(ep)
	}

	return &cluster.ClusterLoadAssignment{
		ClusterName: "purelb", // FIXME
		Endpoints:   endpoints,
	}
}

// repToEndpoint translates one of our egwv1.RemoteEndpoint into one
// of Envoy's endpoint.LocalityLbEndpoints.
func repToEndpoint(ep egwv1.RemoteEndpoint) *endpoint.LocalityLbEndpoints {
	return &endpoint.LocalityLbEndpoints{
		LbEndpoints: []*endpoint.LbEndpoint{{
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
		}},
	}
}

// ServiceToSnapshot translates one of our egwv1.LoadBalancers into an
// xDS cachev2.Snapshot.
func ServiceToSnapshot(version int, service egwv1.LoadBalancer, endpoints []egwv1.RemoteEndpoint) cachev2.Snapshot {
	return cachev2.NewSnapshot(
		strconv.Itoa(version),
		[]types.Resource{serviceToCLA(service, endpoints)}, // endpoints
		[]types.Resource{},                                 // clusters
		[]types.Resource{},                                 // routes
		[]types.Resource{},                                 // listeners
		[]types.Resource{},                                 // runtimes
		[]types.Resource{},                                 // secrets
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
