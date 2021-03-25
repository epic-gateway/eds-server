The EDS server implements an Envoy xDS control plane server whose
configuration is fed by a k8s operator. The data model that the
operator watches is defined in
https://gitlab.com/acnodal/epic/resource-model. The xDS control plane
code is based on https://github.com/envoyproxy/go-control-plane. Other
than those two, most of the code translates from our custom resource
models to Envoy configurations.

Run "make" to get a list of targets.

## Developer Setup

Make sure that you've followed the epic/resource-model
[Developer Setup instructions](https://gitlab.com/acnodal/epic/resource-model/-/tree/egw-to-epic#developer-setup)
because this project also uses private golang modules so it won't
build unless you set up your system properly.
