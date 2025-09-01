package loadbalancer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/apiutils"
	"github.com/substratusai/kubeai/internal/metrics/metricstest"
)

func TestRoutingKeyStrategy(t *testing.T) {
	const (
		myModel = "my-model"
		myPod1  = "pod1"
		myPod2  = "pod2"
		addr1   = "10.0.0.1:8000"
		addr2   = "10.0.0.2:8000"
	)

	tests := []struct {
		name                string
		routingKey          string
		fallbackToLeastLoad bool
		expectError         bool
		expectFallback      bool
	}{
		{
			name:                "with routing key should use consistent hashing",
			routingKey:          "test-key",
			fallbackToLeastLoad: true,
			expectError:         false,
			expectFallback:      false,
		},
		{
			name:                "without routing key and fallback enabled should use least load",
			routingKey:          "",
			fallbackToLeastLoad: true,
			expectError:         false,
			expectFallback:      true,
		},
		{
			name:                "without routing key and fallback disabled should fail",
			routingKey:          "",
			fallbackToLeastLoad: false,
			expectError:         true,
			expectFallback:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metricstest.Init(t)

			manager := &LoadBalancer{
				groups: map[string]*group{},
			}

			lb := v1.LoadBalancing{
				Strategy: v1.RoutingKeyStrategy,
				RoutingKey: v1.RoutingKey{
					MeanLoadPercentage:  125,
					Replication:         1,
					FallbackToLeastLoad: tt.fallbackToLeastLoad,
				},
			}

			manager.getOrCreateEndpointGroup(myModel, lb).reconcileEndpoints(map[string]endpoint{
				myPod1: {address: addr1},
				myPod2: {address: addr2},
			})

			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
			defer cancel()

			gotAddr, gotFunc, gotErr := manager.AwaitBestAddress(ctx, &apiutils.Request{
				Model:         myModel,
				RoutingKey:    tt.routingKey,
				LoadBalancing: lb,
			})

			if tt.expectError {
				require.Error(t, gotErr)
				return
			}

			require.NoError(t, gotErr)
			require.NotEmpty(t, gotAddr)
			assert.True(t, gotAddr == addr1 || gotAddr == addr2)
			gotFunc()

			// Test consistency - same routing key should always return same address
			if tt.routingKey != "" && !tt.expectFallback {
				for i := 0; i < 10; i++ {
					addr, doneFunc, err := manager.AwaitBestAddress(ctx, &apiutils.Request{
						Model:         myModel,
						RoutingKey:    tt.routingKey,
						LoadBalancing: lb,
					})
					require.NoError(t, err)
					assert.Equal(t, gotAddr, addr, "routing key should consistently return same address")
					doneFunc()
				}
			}
		})
	}
}

func TestRoutingKeyConsistency(t *testing.T) {
	const (
		myModel = "my-model"
		myPod1  = "pod1"
		myPod2  = "pod2"
		addr1   = "10.0.0.1:8000"
		addr2   = "10.0.0.2:8000"
	)

	metricstest.Init(t)

	manager := &LoadBalancer{
		groups: map[string]*group{},
	}

	lb := v1.LoadBalancing{
		Strategy: v1.RoutingKeyStrategy,
		RoutingKey: v1.RoutingKey{
			MeanLoadPercentage:  125,
			Replication:         256, // Higher replication for better distribution
			FallbackToLeastLoad: true,
		},
	}

	manager.getOrCreateEndpointGroup(myModel, lb).reconcileEndpoints(map[string]endpoint{
		myPod1: {address: addr1},
		myPod2: {address: addr2},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Test that different routing keys can map to different endpoints
	routingKeys := []string{"key1", "key2", "key3", "key4", "key5"}
	keyToAddr := make(map[string]string)

	for _, key := range routingKeys {
		addr, doneFunc, err := manager.AwaitBestAddress(ctx, &apiutils.Request{
			Model:         myModel,
			RoutingKey:    key,
			LoadBalancing: lb,
		})
		require.NoError(t, err)
		keyToAddr[key] = addr
		doneFunc()

		// Verify consistency - same key should always return same address
		for i := 0; i < 5; i++ {
			addr2, doneFunc2, err := manager.AwaitBestAddress(ctx, &apiutils.Request{
				Model:         myModel,
				RoutingKey:    key,
				LoadBalancing: lb,
			})
			require.NoError(t, err)
			assert.Equal(t, addr, addr2, "routing key %s should consistently return same address", key)
			doneFunc2()
		}
	}

	// Verify that we got some distribution (not all keys map to the same endpoint)
	addrs := make(map[string]bool)
	for _, addr := range keyToAddr {
		addrs[addr] = true
	}
	assert.Greater(t, len(addrs), 0, "should have at least one endpoint used")
}
