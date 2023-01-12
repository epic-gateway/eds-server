package controllers

import (
	"context"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerCallbacks are how controllers notify the control plane
// of object changes.
type LoadBalancerCallbacks interface {
	EndpointChanged(*epicv1.LoadBalancer, []epicv1.RemoteEndpoint) error
	DeleteNode(string, string)
}

// RouteCallbacks are how controllers notify the control plane of
// object changes.
type RouteCallbacks interface {
	UpdateProxy(context.Context, client.Client, *epicv1.GWProxy) error
	DeleteNode(string, string)
}
