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

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"

	epicv1 "gitlab.com/acnodal/epic/resource-model/api/v1"
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
	Endpoints   []epicv1.RemoteEndpoint
}

func unmarshalYAMLCLA(str string, cla *endpoint.ClusterLoadAssignment) error {
	b, err := yaml.YAMLToJSON([]byte(str))
	if err != nil {
		return fmt.Errorf("Error converting yaml to json: '%s'", err)
	}

	if err := jsonpb.Unmarshal(bytes.NewReader(b), types.Resource(cla)); err != nil {
		return fmt.Errorf("Error deserializing resource: '%s'", err)
	}

	return nil
}

// repsToCLAs translates our LoadBalancer service CR into an Envoy
// ClusterLoadAssignment, using a template in the LB Spec.
func repsToCLAs(templateText string, reps []epicv1.RemoteEndpoint) ([]types.Resource, error) {
	var (
		err  error
		clas []types.Resource = []types.Resource{}
	)

	// Get the Template ready to execute.
	tmpl := &template.Template{}
	if tmpl, err = template.New("cla").Funcs(funcMap).Parse(templateText); err != nil {
		return clas, err
	}

	// Scan the reps to find the set of clusters that they reference.
	clusters := map[string]struct{}{}
	for _, rep := range reps {
		clusters[rep.Spec.Cluster] = struct{}{}
	}

	for clName := range clusters {
		cla := endpoint.ClusterLoadAssignment{}

		// Give the Template its parameters and execute it.
		doc := bytes.Buffer{}
		if err := tmpl.Execute(&doc, claParams{
			ClusterName: clName,
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

		clas = append(clas, &cla)
	}

	return clas, nil
}

// repsForCluster figures out which reps belong to the cluster
// named "cluster".
func repsForCluster(reps []epicv1.RemoteEndpoint, cluster string) []epicv1.RemoteEndpoint {
	clusterReps := []epicv1.RemoteEndpoint{}

	for _, rep := range reps {
		if rep.Spec.Cluster == cluster {
			clusterReps = append(clusterReps, rep)
		}
	}

	return clusterReps
}

// RepsToSnapshot translates one of our epicv1.LoadBalancers and its
// reps into an xDS cache.Snapshot. The Snapshot contains only the
// endpoints.
func RepsToSnapshot(version int, template string, reps []epicv1.RemoteEndpoint) (cache.Snapshot, error) {
	clas, err := repsToCLAs(template, reps)
	if err != nil {
		return cache.Snapshot{}, err
	}

	return cache.NewSnapshot(
		strconv.Itoa(version),
		clas,               // endpoints
		[]types.Resource{}, // clusters
		[]types.Resource{}, // routes
		[]types.Resource{}, // listeners
		[]types.Resource{}, // runtimes
		[]types.Resource{}, // secrets
	), nil
}
