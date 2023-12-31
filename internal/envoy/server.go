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
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	runtime "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	server "github.com/envoyproxy/go-control-plane/pkg/server/v3"
)

const (
	// The gRPC golang library sets a very small upper bound for the
	// number of gRPC/h2 streams over a single TCP connection. If a
	// proxy multiplexes requests to the management server over a single
	// connection, then it might lead to availability problems.
	grpcMaxConcurrentStreams = 1000000
)

func registerServer(grpcServer *grpc.Server, xDSServer server.Server) {
	// register services
	endpoint.RegisterEndpointDiscoveryServiceServer(grpcServer, xDSServer)
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, xDSServer)
	runtime.RegisterRuntimeDiscoveryServiceServer(grpcServer, xDSServer)
}

// runServer starts "xDSServer" on "port".
func runServer(ctx context.Context, xDSServer server.Server, port uint, tlsConfig *tls.Config) error {
	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams),
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}

	registerServer(grpcServer, xDSServer)

	return grpcServer.Serve(lis)
}
