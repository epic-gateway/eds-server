package envoy

import (
	"bytes"
	"fmt"
	"html/template"
	"strconv"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/jsonpb"
	v1 "k8s.io/api/core/v1"

	cluster "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev2 "github.com/envoyproxy/go-control-plane/pkg/cache/v2"

	egwv1 "gitlab.com/acnodal/egw-resource-model/api/v1"
)

var (
	funcMap = template.FuncMap{
		// This function is used by the template to ensure that protocol
		// names (e.g., "TCP") are always in caps.
		"ToUpper": func(protocol v1.Protocol) string {
			return strings.ToUpper(string(protocol))
		},
	}
)

// The parameters that we pass into the template.
type claParams struct {
	ClusterName string
	ServiceName string
	Endpoints   []egwv1.RemoteEndpoint
}

func unmarshalYAMLCLA(str string, cla *cluster.ClusterLoadAssignment) error {
	b, err := yaml.YAMLToJSON([]byte(str))
	if err != nil {
		return fmt.Errorf("Error converting yaml to json: '%s'", err)
	}

	if err := jsonpb.Unmarshal(bytes.NewReader(b), types.Resource(cla)); err != nil {
		return fmt.Errorf("Error deserializing resource: '%s'", err)
	}

	return nil
}

// serviceToCLAs translates our LoadBalancer service CR into an Envoy
// ClusterLoadAssignment, using a template in the LB Spec.
func serviceToCLAs(service *egwv1.LoadBalancer, reps []egwv1.RemoteEndpoint) ([]types.Resource, error) {
	var (
		err  error
		clas []types.Resource = make([]types.Resource, len(service.Spec.UpstreamClusters))
	)

	// Get the Template ready to execute.
	tmpl := &template.Template{}
	tmplText := service.Spec.EnvoyTemplate.EnvoyResources.Endpoints[0].Value
	if tmpl, err = template.New("cla").Funcs(funcMap).Parse(tmplText); err != nil {
		return clas, err
	}

	for i, clName := range service.Spec.UpstreamClusters {
		cla := cluster.ClusterLoadAssignment{}

		// Give the Template its parameters and execute it.
		doc := bytes.Buffer{}
		if err := tmpl.Execute(&doc, claParams{
			ClusterName: clName,
			ServiceName: service.Name,
			Endpoints:   repsForCluster(reps, clName),
		}); err != nil {
			return clas, err
		}

		// The output of the Template is a String, but we need to provide a
		// Golang ClusterLoadAssignment to the cache, so we need to
		// unmarshal it.
		if err := unmarshalYAMLCLA(doc.String(), &cla); err != nil {
			return clas, err
		}

		clas[i] = &cla
	}

	return clas, nil
}

// repsForCluster figures out which reps belong to the cluster
// named "cluster".
func repsForCluster(reps []egwv1.RemoteEndpoint, cluster string) []egwv1.RemoteEndpoint {
	clusterReps := []egwv1.RemoteEndpoint{}

	for _, rep := range reps {
		if rep.Spec.Cluster == cluster {
			clusterReps = append(clusterReps, rep)
		}
	}

	return clusterReps
}

// ServiceToSnapshot translates one of our egwv1.LoadBalancers and its
// reps into an xDS cachev2.Snapshot. The Snapshot contains only the
// endpoints.
func ServiceToSnapshot(version int, service *egwv1.LoadBalancer, reps []egwv1.RemoteEndpoint) (cachev2.Snapshot, error) {
	clas, err := serviceToCLAs(service, reps)
	if err != nil {
		return cachev2.Snapshot{}, err
	}

	return cachev2.NewSnapshot(
		strconv.Itoa(version),
		clas,               // endpoints
		[]types.Resource{}, // clusters
		[]types.Resource{}, // routes
		[]types.Resource{}, // listeners
		[]types.Resource{}, // runtimes
		[]types.Resource{}, // secrets
	), nil
}
