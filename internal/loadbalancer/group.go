package loadbalancer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	v1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/apiutils"
)

func newEndpointGroup(lb v1.LoadBalancing) *group {
	// Use RoutingKey replication if RoutingKey strategy, otherwise use PrefixHash replication
	replication := lb.PrefixHash.Replication
	if lb.Strategy == v1.RoutingKeyStrategy {
		replication = lb.RoutingKey.Replication
	}

	// Ensure we have a minimum replication of 1 for hash-based strategies
	if (lb.Strategy == v1.PrefixHashStrategy || lb.Strategy == v1.RoutingKeyStrategy) && replication == 0 {
		replication = 256 // Default replication value
	}

	g := &group{
		endpoints:         make(map[string]endpoint),
		totalInFlight:     &atomic.Int64{},
		chwblReplication:  replication,
		chwblHashes:       map[uint64]string{},
		chwblSortedHashes: []uint64{},
		bcast:             make(chan struct{}),
	}
	return g
}

type group struct {
	mtx sync.RWMutex

	endpoints map[string]endpoint

	totalInFlight *atomic.Int64

	// the number of times an endpoint is replicated on the hash ring
	chwblReplication int
	// map of hash to endpoint
	chwblHashes map[uint64]string
	// sorted list of hashed node-replicas
	chwblSortedHashes []uint64

	bmtx  sync.RWMutex
	bcast chan struct{} // closed when there's a broadcast
}

type endpoint struct {
	address string

	inFlight *atomic.Int64

	adapters map[string]struct{}
}

// getBestAddr returns the best "IP:Port". It blocks until there are available endpoints
// in the endpoint group.
func (g *group) getBestAddr(ctx context.Context, req *apiutils.Request, awaitChangeEndpoints bool) (string, func(), error) {
	g.mtx.RLock()
	// await endpoints exists
	for awaitChangeEndpoints || len(g.endpoints) == 0 {
		g.mtx.RUnlock()
		select {
		case <-g.awaitEndpoints():
		case <-ctx.Done():
			return "", func() {}, ctx.Err()
		}
		g.mtx.RLock()
	}

	var ep endpoint
	var found bool
	switch req.LoadBalancing.Strategy {
	case v1.PrefixHashStrategy:
		ep, found = g.chwblGetAddr(req.Adapter+req.Prefix, float64(req.LoadBalancing.PrefixHash.MeanLoadPercentage)/100, req.Adapter)
	case v1.RoutingKeyStrategy:
		ep, found = g.routingKeyGetAddr(req, float64(req.LoadBalancing.RoutingKey.MeanLoadPercentage)/100, req.Adapter)
	case v1.LeastLoadStrategy:
		ep, found = g.getAddrLeastLoad(req.Adapter)
	default:
		return "", func() {}, fmt.Errorf("unknown load balancing strategy: %v", req.LoadBalancing.Strategy)
	}

	if !found {
		g.mtx.RUnlock()
		return g.getBestAddr(ctx, req, true)
	}

	g.addInFlight(ep.inFlight, 1)
	decFunc := func() {
		g.addInFlight(ep.inFlight, -1)
	}
	g.mtx.RUnlock()
	return ep.address, decFunc, nil
}

func (g *group) awaitEndpoints() chan struct{} {
	g.bmtx.RLock()
	defer g.bmtx.RUnlock()
	return g.bcast
}

func (g *group) getAllAddrs() []string {
	g.mtx.RLock()
	defer g.mtx.RUnlock()

	var hosts []string
	for _, ep := range g.endpoints {
		hosts = append(hosts, ep.address)
	}

	return hosts
}

func (g *group) reconcileEndpoints(observed map[string]endpoint) {
	g.mtx.Lock()
	for name, observedEp := range observed {
		if currentEp, ok := g.endpoints[name]; ok {
			currentEp.adapters = observedEp.adapters
			g.endpoints[name] = currentEp
		} else {
			g.endpoints[name] = endpoint{
				inFlight: &atomic.Int64{},
				address:  observedEp.address,
				adapters: observedEp.adapters,
			}
			g.chwblAddEndpoint(name)
		}
	}
	for name := range g.endpoints {
		if _, ok := observed[name]; !ok {
			// NOTE: No need to decrement in-flight counts, that will happen
			// when requests complete or fail.
			g.chwblRemoveEndpoint(name)
			delete(g.endpoints, name)
		}
	}
	g.mtx.Unlock()

	// notify waiting requests
	if len(observed) > 0 {
		g.broadcastEndpoints()
	}
}

func (g *group) broadcastEndpoints() {
	g.bmtx.Lock()
	defer g.bmtx.Unlock()

	close(g.bcast)
	g.bcast = make(chan struct{})
}

func (g *group) addInFlight(endpointInFlight *atomic.Int64, add int64) int64 {
	g.totalInFlight.Add(add)
	return endpointInFlight.Add(add)
}
