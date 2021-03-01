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
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	serverv2 "github.com/envoyproxy/go-control-plane/pkg/server/v2"
)

const (
	grpcMaxConcurrentStreams = 1000000
)

func registerServer(grpcServer *grpc.Server, server serverv2.Server) {
	// register services
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	discoverygrpc.RegisterRuntimeDiscoveryServiceServer(grpcServer, server)
}

// starts an xDS server on the given port
func runServer(ctx context.Context, srv2 serverv2.Server, port uint) {
	// gRPC golang library sets a very small upper bound for the number gRPC/h2
	// streams over a single TCP connection. If a proxy multiplexes requests over
	// a single connection to the management server, then it might lead to
	// availability problems.
	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams))
	grpcServer := grpc.NewServer(grpcOptions...)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}

	registerServer(grpcServer, srv2)

	log.Printf("management server listening on %d\n", port)
	if err = grpcServer.Serve(lis); err != nil {
		log.Println(err)
	}
}
