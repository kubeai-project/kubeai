package loadbalancer

import (
	"context"
	"fmt"

	"github.com/substratusai/kubeai/internal/apiutils"
	"github.com/substratusai/kubeai/internal/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (g *group) routingKeyGetAddr(req *apiutils.Request, loadFactor float64, adapter string) (endpoint, bool) {
	// If no routing key is provided, handle fallback behavior
	if req.RoutingKey == "" {
		if req.LoadBalancing.RoutingKey.FallbackToLeastLoad {
			// Fall back to least load strategy
			return g.getAddrLeastLoad(adapter)
		}
		// Return no endpoint found, which will cause the request to fail
		return endpoint{}, false
	}

	// Use the routing key for consistent hashing
	if len(g.chwblHashes) == 0 {
		return endpoint{}, false
	}

	// Hash the routing key (with adapter if present for additional uniqueness)
	key := req.RoutingKey
	if adapter != "" {
		key = adapter + req.RoutingKey
	}

	h := chwblHash(key)
	hash0, i0 := g.chwblSearch(h)

	{
		name0 := g.chwblHashes[hash0]
		metrics.InferenceRequestsHashLookupInitial.Add(context.Background(), 1, metric.WithAttributeSet(attribute.NewSet(
			metrics.AttrEndpoint.String(name0),
		)))
	}

	// The default endpoint is the endpoint that is able to serve the request (has the adapter)
	// but might not meet the load requirement after all other endpoints have been checked.
	var defaultEndpoint *endpoint

	i := i0
	// Avoid an infinite loop by checking if we've checked all the endpoints.
	var defaultEndpointName string
	for n := 0; n < len(g.chwblSortedHashes); n++ {
		name := g.chwblHashes[g.chwblSortedHashes[i]]
		ep, ok := g.endpoints[name]
		if !ok {
			panic(fmt.Sprintf("endpoints corrupted, %q should be in map", name))
		}

		var adapterMatches bool
		if adapter == "" {
			adapterMatches = true
		} else {
			_, adapterMatches = ep.adapters[adapter]
		}

		if adapterMatches {
			if defaultEndpoint == nil {
				// Save the first endpoint that has the adapter in case no
				// endpoint is found with acceptable load.
				defaultEndpoint = &ep
				defaultEndpointName = name
			}
			if chwblLoadOK(ep.inFlight.Load(), g.totalInFlight.Load(), len(g.endpoints), loadFactor) {
				metrics.InferenceRequestsHashLookupIterations.Record(context.Background(), int64(n+1))
				metrics.InferenceRequestsHashLookupFinal.Add(context.Background(), 1, metric.WithAttributeSet(attribute.NewSet(
					metrics.AttrEndpoint.String(name),
				)))
				return ep, true
			}
		}

		i++
		if i >= len(g.chwblSortedHashes) {
			// wrap around
			i = 0
		}
	}

	if defaultEndpoint != nil {
		metrics.InferenceRequestsHashLookupIterations.Record(context.Background(), int64(len(g.chwblSortedHashes)))
		metrics.InferenceRequestsHashLookupFinal.Add(context.Background(), 1, metric.WithAttributeSet(attribute.NewSet(
			metrics.AttrEndpoint.String(defaultEndpointName),
		)))
		metrics.InferenceRequestsHashLookupDefault.Add(context.Background(), 1, metric.WithAttributeSet(attribute.NewSet(
			metrics.AttrEndpoint.String(defaultEndpointName),
		)))
		return *defaultEndpoint, true
	}
	return endpoint{}, false
}
