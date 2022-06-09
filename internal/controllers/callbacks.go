package controllers

import (
	"context"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RouteCallbacks are how controllers notify the control plane of
// object changes.
type RouteCallbacks interface {
	UpdateProxy(context.Context, client.Client, *epicv1.GWProxy) error
	DeleteNode(string, string)
}
