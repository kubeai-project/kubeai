package loadbalancer

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/kubeai-project/kubeai/internal/metrics/metricstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAwaitBestHostBehavior(t *testing.T) {
	const (
		myModel              = "my-model"
		myAdapter            = "my-adapter"
		myPodWithoutAdapter  = "pod1"
		myPodWithAdapter     = "pod2"
		myAddrWithoutAdapter = "10.0.0.1:8000"
		myAddrWithAdapter    = "10.0.0.2:8000"
	)

	testCases := map[string]struct {
		model      string
		adapter    string
		endpoints  map[string]endpoint
		strategies []v1.LoadBalancingStrategy
		expAddr    string
		expErr     error
	}{
		"model only": {
			model: myModel,
			strategies: []v1.LoadBalancingStrategy{
				v1.LeastLoadStrategy,
				v1.PrefixHashStrategy,
			},
			expAddr: myAddrWithoutAdapter,
			endpoints: map[string]endpoint{
				myPodWithoutAdapter: {address: myAddrWithoutAdapter},
			},
		},
		"model and adapter": {
			model:   myModel,
			adapter: myAdapter,
			endpoints: map[string]endpoint{
				myPodWithoutAdapter: {
					address: myAddrWithoutAdapter,
				},
				myPodWithAdapter: {
					address: myAddrWithAdapter,
					adapters: map[string]struct{}{
						myAdapter: {},
					}},
			},
			strategies: []v1.LoadBalancingStrategy{
				v1.LeastLoadStrategy,
				v1.PrefixHashStrategy,
			},
			expAddr: myAddrWithAdapter,
		},
		"no matching model blocks until timeout": {
			model: "unknown-model",
			endpoints: map[string]endpoint{
				myPodWithoutAdapter: {address: myAddrWithoutAdapter},
			},
			strategies: []v1.LoadBalancingStrategy{
				v1.LeastLoadStrategy,
				v1.PrefixHashStrategy,
			},
			expErr: context.DeadlineExceeded,
		},
		"no matching adapter blocks until timeout": {
			model:   myModel,
			adapter: "unknown-adapter",
			endpoints: map[string]endpoint{
				myPodWithoutAdapter: {address: myAddrWithoutAdapter},
			},
			strategies: []v1.LoadBalancingStrategy{
				v1.LeastLoadStrategy,
				v1.PrefixHashStrategy,
			},
			expErr: context.DeadlineExceeded,
		},
		// not covered: unknown port with multiple ports on entrypoint
	}

	for name, spec := range testCases {
		for _, strategy := range spec.strategies {
			t.Run(name+" with "+string(strategy)+" strategy", func(t *testing.T) {
				metricstest.Init(t)

				manager := &LoadBalancer{
					groups: map[string]*group{},
				}

				lb := v1.LoadBalancing{
					Strategy: strategy,
					PrefixHash: v1.PrefixHash{
						MeanLoadPercentage: 125,
						Replication:        1,
					},
				}
				manager.getOrCreateEndpointGroup(myModel, lb).reconcileEndpoints(spec.endpoints)

				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()

				gotAddr, gotFunc, gotErr := manager.AwaitBestAddress(ctx, &apiutils.Request{
					Model:         spec.model,
					Adapter:       spec.adapter,
					LoadBalancing: lb,
				})
				if spec.expErr != nil {
					require.ErrorIs(t, spec.expErr, gotErr)
					return
				}
				require.NoError(t, gotErr)
				gotFunc()
				assert.Equal(t, spec.expAddr, gotAddr)
			})
		}
	}
}

func TestLoadBalancingStrategies(t *testing.T) {
	const (
		modelA = "model-a"
		modelB = "model-b"

		adapterA1 = "adapter-a-1"
		adapterA2 = "adapter-a-2"

		podA1Name = "pod-a-1"
		podA1Addr = "10.0.0.1:8000"

		podA2Name = "pod-a-2"
		podA2Addr = "10.0.0.2:8000"

		podB1Name = "pod-b-1"
		podB1Addr = "10.0.0.3:8000"

		podB2Name = "pod-b-2"
		podB2Addr = "10.0.0.4:8000"
	)

	var (
		podA1Hash = chwblEndpointReplicaHashInput(podA1Name, 0)
		podA2Hash = chwblEndpointReplicaHashInput(podA2Name, 0)
	)

	type testStep struct {
		name string

		requestCount int
		model        string
		adapter      string
		prefix       string

		expectedAddrCounts map[string]int
		completeForAddrs   map[string]int
	}
	cases := []struct {
		name string
		// map[<model-name>]map[<endpoint-name>]<endpoint>
		modelEndpoints map[string]map[string]endpoint
		// map[<model-name>]map[<endpoint-name>]<in-flight-count>
		initialInFlight map[string]map[string]int64
		loadBalancing   v1.LoadBalancing
		steps           []testStep
	}{
		{
			name: "least load strategy",
			modelEndpoints: map[string]map[string]endpoint{
				modelA: {
					podA1Name: {address: podA1Addr, adapters: map[string]struct{}{adapterA1: {}}},
					podA2Name: {address: podA2Addr, adapters: map[string]struct{}{adapterA2: {}}},
				},
				modelB: {
					podB1Name: {address: podB1Addr},
					podB2Name: {address: podB2Addr},
				},
			},
			loadBalancing: v1.LoadBalancing{
				Strategy: v1.LeastLoadStrategy,
			},
			steps: []testStep{
				{
					name:         "first 2 requests to model-a",
					model:        modelA,
					requestCount: 2,
					expectedAddrCounts: map[string]int{
						podA1Addr: 1,
						podA2Addr: 1,
					},
				},
				{
					name:         "a lot more requests to model-a",
					model:        modelA,
					requestCount: 100,
					expectedAddrCounts: map[string]int{
						podA1Addr: 50,
						podA2Addr: 50,
					},
				},
				{
					name:         "requests to model-a adapter-a-1",
					model:        modelA,
					adapter:      adapterA1,
					requestCount: 50,
					expectedAddrCounts: map[string]int{
						podA1Addr: 50,
					},
				},
				{
					name:         "requests to model-a without adapter should be distributed to the other pod",
					model:        modelA,
					requestCount: 52,
					expectedAddrCounts: map[string]int{
						podA1Addr: 1,
						podA2Addr: 51,
					},
				},
				{
					name:         "back to even balance",
					model:        modelA,
					requestCount: 2,
					expectedAddrCounts: map[string]int{
						podA1Addr: 1,
						podA2Addr: 1,
					},
				},
				{
					name: "complete some request for pod-a-2",
					completeForAddrs: map[string]int{
						podA2Addr: 10,
					},
				},
				{
					name:         "requests to model-a should now be distributed to the other pod",
					model:        modelA,
					requestCount: 12,
					expectedAddrCounts: map[string]int{
						podA1Addr: 1,
						podA2Addr: 11,
					},
				},
				{
					name:         "first requests to model-b",
					model:        modelB,
					requestCount: 2,
					expectedAddrCounts: map[string]int{
						podB1Addr: 1,
						podB2Addr: 1,
					},
				},
			},
		},
		{
			name: "prefix hash strategy",
			modelEndpoints: map[string]map[string]endpoint{
				modelA: {
					podA1Name: {address: podA1Addr},
					podA2Name: {address: podA2Addr},
				},
				modelB: {
					podB1Name: {address: podB1Addr},
				},
			},
			initialInFlight: map[string]map[string]int64{
				modelA: {
					podA1Name: 10,
					podA2Name: 10,
				},
			},
			loadBalancing: v1.LoadBalancing{
				Strategy: v1.PrefixHashStrategy,
				PrefixHash: v1.PrefixHash{
					MeanLoadPercentage: 150,
					Replication:        1,
				},
			},
			steps: []testStep{
				{
					name:         "first request to model-a, preferring pod-a-1, each pod has 10 in-flight requests",
					model:        modelA,
					prefix:       podA1Hash,
					requestCount: 1,
					expectedAddrCounts: map[string]int{
						podA1Addr: 1,
					},
				},
				{
					// load0	load1	1.5*(avg+1)		(load0) <= (thres)
					// 10		10		15.75			TRUE
					// 11		10		16.5			TRUE
					// 12		10		17.25			TRUE
					// 13		10		18				TRUE
					// 14		10		18.75			TRUE
					// 15		10		19.5			TRUE
					// 16		10		20.25			TRUE
					// 17		10		21				TRUE
					// 18		10		21.75			TRUE
					// 19		10		22.5			TRUE
					// 20		10		23.25			TRUE
					// 21		10		24				TRUE
					// 22		10		24.75			TRUE
					// 23		10		25.5			TRUE
					// 24		10		26.25			TRUE
					// 25		10		27				TRUE
					// 26		10		27.75			TRUE
					// 27		10		28.5			TRUE
					// 28		10		29.25			TRUE
					// 29		10		30				TRUE
					// 30		10		30.75			TRUE
					name:  "20 more requests preferring pod-a-1",
					model: modelA,
					// By making sure that the prefix matches the input used to hash the endpoint (pod-a-1),
					// we can ensure that the algorithm will prefer pod-a-1.
					prefix:       podA1Hash,
					requestCount: 20,
					// See the table above for the expected distribution.
					expectedAddrCounts: map[string]int{
						podA1Addr: 20,
					},
				},
				{
					// 30	10	30.75	TRUE
					// 31	10	31.5	FALSE <-- 1 request (starting here)
					// 31	11	32.25	TRUE  <-- 2 requests
					// 32	11	33		TRUE  <-- 3 requests
					// 33	11	33.75	TRUE  <-- 4 requests
					name:         "4 more requests preferring pod-a-1",
					model:        modelA,
					prefix:       podA1Hash,
					requestCount: 4,
					// See the table above for the expected distribution.
					expectedAddrCounts: map[string]int{
						podA1Addr: 3,
						podA2Addr: 1,
					},
				},
				{
					name:         "with pod-a-1 near max load, requests preferring pod-a-2 should be distributed to pod-a-2",
					model:        modelA,
					prefix:       podA2Hash,
					requestCount: 20,
					expectedAddrCounts: map[string]int{
						podA2Addr: 20,
					},
				},
				{
					name:  "requests to model-b should be distributed to pod-b-1, as it is the only endpoint",
					model: modelB,
					// Use a hash that doesn't match any of the endpoints in model-b
					// but does for model-a (to test hash-ring separation by model).
					prefix:       podA2Hash,
					requestCount: 100_000,
					expectedAddrCounts: map[string]int{
						podB1Addr: 100_000,
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			manager := &LoadBalancer{
				groups: map[string]*group{},
			}

			for model, endpoints := range c.modelEndpoints {
				manager.getOrCreateEndpointGroup(model, c.loadBalancing).reconcileEndpoints(endpoints)
			}

			for modelName, inFlight := range c.initialInFlight {
				for endpointName, count := range inFlight {
					g, _ := manager.getEndpointGroup(modelName)
					ep := g.endpoints[endpointName]
					g.addInFlight(ep.inFlight, count)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			doneFuncs := map[string][]func(){}
			for _, step := range c.steps {
				counts := map[string]int{}
				for i := 0; i < step.requestCount; i++ {
					// fmt.Println("request: ", step.name, "i: ", i)
					addr, done, err := manager.AwaitBestAddress(ctx, &apiutils.Request{
						Model:         step.model,
						Adapter:       step.adapter,
						Prefix:        step.prefix,
						LoadBalancing: c.loadBalancing,
					})
					require.NoError(t, err, "request: "+step.name)
					doneFuncs[addr] = append(doneFuncs[addr], done)
					counts[addr]++
				}
				if step.expectedAddrCounts != nil {
					require.Equalf(t, step.expectedAddrCounts, counts, "request: %s", step.name)
				}

				for addr, count := range step.completeForAddrs {
					for i := 0; i < count; i++ {
						doneFuncs[addr][i]()
						// remove the done function from the list
						doneFuncs[addr] = doneFuncs[addr][1:]
					}
				}
			}

			for _, dones := range doneFuncs {
				for _, done := range dones {
					done()
				}
			}
		})
	}
}

// TestAwaitbestHostParallelAccounting tests that running AwaitBestHost in parallel
// produces and expected distribution of endpoint results.
func TestAwaitBestHostParallelAccounting(t *testing.T) {
	const (
		myModel   = "my-model"
		myAdapter = "my-adapter"
		myPod0    = "pod0"
		myPod1    = "pod1"
		pod0Addr  = "10.0.0.1:8000"
		pod1Addr  = "10.0.0.2:8000"
	)

	const m = 10
	for i := 0; i < m; i++ {
		for _, strategy := range []v1.LoadBalancingStrategy{
			v1.LeastLoadStrategy,
			v1.PrefixHashStrategy,
		} {
			t.Run(fmt.Sprintf("%s-%d", strategy, i), func(t *testing.T) {
				metricstest.Init(t)

				manager := &LoadBalancer{
					groups: map[string]*group{},
				}

				lb := v1.LoadBalancing{
					Strategy: strategy,
					PrefixHash: v1.PrefixHash{
						// Check that the distribution is roughly even without
						// the MLP threshold taking effect.
						MeanLoadPercentage: 100000,
						Replication:        256,
						PrefixCharLength:   1000,
					},
				}
				manager.getOrCreateEndpointGroup(myModel, lb).reconcileEndpoints(map[string]endpoint{
					myPod0: {address: pod0Addr},
					myPod1: {address: pod1Addr},
				})

				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()

				var wg, awaitGroup sync.WaitGroup
				var doneMtx sync.RWMutex
				doneMtx.Lock()
				const n = 100_000
				rnd := rand.New(rand.NewPCG(uint64(i), 0))
				for j := 0; j < n; j++ {
					wg.Add(1)
					awaitGroup.Add(1)
					go func() {
						_, doneFunc, err := manager.AwaitBestAddress(ctx, &apiutils.Request{
							Model:         myModel,
							Adapter:       "",
							LoadBalancing: lb,
							Prefix:        fmt.Sprintf("%d", rnd.IntN(100_000_000)),
						})
						require.NoError(t, err)
						awaitGroup.Done()
						defer func() {
							doneMtx.RLock()
							defer doneMtx.RUnlock()
							doneFunc()
							wg.Done()
						}()
					}()
				}

				awaitGroup.Wait()
				g := manager.groups[myModel]
				require.EqualValues(t, n, g.totalInFlight.Load())
				ep0Val := g.endpoints[myPod0].inFlight.Load()
				ep1Val := g.endpoints[myPod1].inFlight.Load()
				require.Less(t, percentDifference(float64(ep0Val), float64(ep1Val)), 4.0)

				doneMtx.Unlock()
				wg.Wait()

				require.EqualValues(t, 0, g.totalInFlight.Load())
				for _, ep := range g.endpoints {
					require.EqualValues(t, 0, ep.inFlight.Load())
				}
			})
		}
	}
}

func percentDifference(a, b float64) float64 {
	// Avoid division by zero if a + b == 0
	if (a + b) == 0 {
		return 0
	}

	diff := math.Abs(a - b)
	avg := (a + b) / 2.0
	return (diff / avg) * 100.0
}
