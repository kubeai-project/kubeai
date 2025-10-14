package loadbalancer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/util/rand"
)

func TestConcurrentAccess(t *testing.T) {
	const (
		myModel = "myModel"
		myAddr  = "10.0.0.1:8000"
	)

	testCases := map[string]struct {
		readerCount int
		writerCount int
	}{
		"one reader_one_writer": {readerCount: 1, writerCount: 1},
		"lot of reader":         {readerCount: 1_000, writerCount: 1},
		"lot of writer":         {readerCount: 1, writerCount: 1_000},
		"lot of both":           {readerCount: 1_000, writerCount: 1_000},
	}
	for name, spec := range testCases {
		randomReadFn := []func(g *group){
			func(g *group) {
				ip, f, err := g.getBestAddr(context.Background(), &apiutils.Request{
					Model: myModel,
					LoadBalancing: v1.LoadBalancing{
						Strategy: v1.LeastLoadStrategy,
					},
				}, false)
				require.NoError(t, err)
				defer f()
				assert.Equal(t, myAddr, ip)
			},
			func(g *group) { g.getAllAddrs() },
		}
		t.Run(name, func(t *testing.T) {
			// setup endpoint with one endpoint so that requests are not waiting
			group := newEndpointGroup(v1.LoadBalancing{PrefixHash: v1.PrefixHash{Replication: 100}})
			group.reconcileEndpoints(
				map[string]endpoint{myModel: {address: myAddr}},
			)

			var startWg, doneWg sync.WaitGroup
			startWg.Add(spec.readerCount + spec.writerCount)
			doneWg.Add(spec.readerCount + spec.writerCount)
			startTogether := func(n int, f func()) {
				for i := 0; i < n; i++ {
					go func() {
						startWg.Done()
						startWg.Wait()
						f()
						doneWg.Done()
					}()
				}
			}
			// when
			startTogether(spec.readerCount, func() { randomReadFn[rand.Intn(len(randomReadFn)-1)](group) })
			startTogether(spec.writerCount, func() {
				group.reconcileEndpoints(
					map[string]endpoint{myModel: {address: myAddr}},
				)
			})
			doneWg.Wait()
		})
	}
}

func TestBlockAndWaitForEndpoints(t *testing.T) {
	var completed atomic.Int32
	var startWg, doneWg sync.WaitGroup
	startTogether := func(n int, f func()) {
		startWg.Add(n)
		doneWg.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				startWg.Done()
				startWg.Wait()
				f()
				completed.Add(1)
				doneWg.Done()
			}()
		}
	}
	group := newEndpointGroup(v1.LoadBalancing{PrefixHash: v1.PrefixHash{Replication: 100}})
	ctx := context.TODO()
	startTogether(100, func() {
		group.getBestAddr(ctx, &apiutils.Request{}, false)
	})
	startWg.Wait()

	// when broadcast triggered
	group.reconcileEndpoints(
		map[string]endpoint{rand.String(4): {}},
	)
	// then
	doneWg.Wait()
	assert.Equal(t, int32(100), completed.Load())
}

func TestAbortOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var startWg, doneWg sync.WaitGroup
	startWg.Add(1)
	doneWg.Add(1)
	go func(t *testing.T) {
		startWg.Wait()
		endpoint := newEndpointGroup(v1.LoadBalancing{PrefixHash: v1.PrefixHash{Replication: 100}})
		_, f, err := endpoint.getBestAddr(ctx, &apiutils.Request{}, false)
		defer f()
		require.Error(t, err)
		doneWg.Done()
	}(t)
	startWg.Done()
	cancel()

	doneWg.Wait()
}
